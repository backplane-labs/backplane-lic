// The license↔user and license↔entitlement relationships: index, show,
// attach and detach.
package licenses

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// UsersPage is the license's user index page.
type UsersPage struct {
	Items []users.Listed
	Count int
}

// Users answers the license's users (owner ∪ licensees).
func (s Scenario) Users(
	ctx context.Context, account domain.Account, idOrKey string, params paging.Params,
) (UsersPage, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return UsersPage{}, err
	}

	if err := s.userRelationship(ctx, account, license); err != nil {
		return UsersPage{}, err
	}

	f := users.Filter{LicenseID: uuid.NullUUID{UUID: license.ID, Valid: true}}

	items, err := s.users.ListUsers(ctx, account.ID, f, "", uuid.Nil, params)
	if err != nil {
		return UsersPage{}, fmt.Errorf("licenses: users: %w", err)
	}

	out := UsersPage{Items: items, Count: len(items)}

	if params.Mode == paging.ModeOffset {
		count, err := s.users.CountUsers(ctx, account.ID, f, "", uuid.Nil)
		if err != nil {
			return UsersPage{}, fmt.Errorf("licenses: users count: %w", err)
		}

		out.Count = count
	}

	return out, nil
}

// User answers one of the license's users by id or email.
func (s Scenario) User(
	ctx context.Context, account domain.Account, idOrKey, userIDOrEmail string,
) (users.Listed, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return users.Listed{}, err
	}

	if err := s.userRelationship(ctx, account, license); err != nil {
		return users.Listed{}, err
	}

	var (
		user domain.User
		id   uuid.UUID
	)

	if uuidRe.MatchString(userIDOrEmail) {
		id = uuid.MustParse(userIDOrEmail)
		user, err = s.users.UserByID(ctx, account.ID, id)
	} else {
		user, err = s.users.UserByEmail(ctx, account.ID, userIDOrEmail)
		id = user.ID
	}

	if err != nil {
		return users.Listed{}, apperr.NotFound("user", userIDOrEmail)
	}

	matches, err := s.store.LicenseUserMatches(ctx, account.ID, license.ID, id, &user.Email)
	if err != nil {
		return users.Listed{}, fmt.Errorf("licenses: user of license: %w", err)
	}

	if !matches {
		return users.Listed{}, apperr.NotFound("user", userIDOrEmail)
	}

	active, err := s.users.UserHasActiveLicense(ctx, account.ID, user.ID)
	if err != nil {
		return users.Listed{}, fmt.Errorf("licenses: user status: %w", err)
	}

	return users.Listed{User: user, Status: user.Status(active, s.now())}, nil
}

// userRelationship runs the shared index/show guard.
func (s Scenario) userRelationship(ctx context.Context, account domain.Account, license domain.License) error {
	if err := access.Require(ctx, "user.read"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}

// AttachUsers attaches licensees, answering the join rows.
func (s Scenario) AttachUsers( //nolint:cyclop // the attach validations, verbatim
	ctx context.Context, account domain.Account, idOrKey string, userIDs []uuid.UUID,
) ([]domain.LicenseUser, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return nil, err
	}

	if err := access.Require(ctx, "license.users.attach"); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseUsersAttach(access.PrincipalOf(ctx), license); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	for _, id := range userIDs {
		if _, err := s.users.UserByID(ctx, account.ID, id); err != nil {
			return nil, apperr.Validation("USER_NOT_FOUND", "must exist",
				"/data/relationships/user", "licenses/#licenses-object-relationships-user")
		}

		if license.OwnerID.Valid && license.OwnerID.UUID == id {
			return nil, apperr.Validation("USER_CONFLICT",
				"already exists (user is attached through owner)",
				"/data/relationships/user", "licenses/#licenses-object-relationships-user")
		}

		attached, err := s.store.LicenseeIsUser(ctx, account.ID, license.ID, id)
		if err != nil {
			return nil, fmt.Errorf("licenses: attach users: %w", err)
		}

		if attached {
			return nil, apperr.Validation("USER_TAKEN", "already exists",
				"/data/relationships/user", "licenses/#licenses-object-relationships-user")
		}
	}

	// Users overage according to the policy's strategy.
	next := license.UsersCount() + int32(len(userIDs)) //nolint:gosec // bounded by the request limit
	if limit := license.MaxUsers(); limit != nil && next > *limit &&
		!overageCovers(license.Policy, float64(next), float64(*limit), !license.Policy.Floating) {
		return nil, apperr.Validation("USER_LIMIT_EXCEEDED",
			fmt.Sprintf("user count has exceeded maximum allowed for license (%d)", *limit),
			"/data/relationships/users", "licenses/#licenses-object-relationships-users")
	}

	var rows []domain.LicenseUser

	err = s.write(ctx, func(ctx context.Context) error {
		if err := s.store.InsertLicenseUsers(ctx, uuid.New(), account.ID, license.ID, userIDs); err != nil {
			return fmt.Errorf("licenses: attach users: %w", err)
		}

		count := license.LicenseUsersCount + int32(len(userIDs)) //nolint:gosec // bounded by the request limit

		if err := s.store.SetLicenseUsersCount(ctx, account.ID, license.ID, count); err != nil {
			return fmt.Errorf("licenses: attach users count: %w", err)
		}

		rows, err = s.store.ListLicenseUserRows(ctx, account.ID, license.ID, userIDs)
		if err != nil {
			return fmt.Errorf("licenses: attached users: %w", err)
		}

		return s.publish(ctx, "license.users.attached", rows)
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // the boundary's own error
	}

	return rows, nil
}

// DetachUsers detaches licensees; the owner and unknown users are
// rejected with the reference's errors.
func (s Scenario) DetachUsers(
	ctx context.Context, account domain.Account, idOrKey string, userIDs []uuid.UUID,
) error {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "license.users.detach"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseUsersAttach(access.PrincipalOf(ctx), license); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if license.OwnerID.Valid {
		for index, id := range userIDs {
			if id == license.OwnerID.UUID {
				return apperr.ForbiddenIndex(
					fmt.Sprintf("cannot detach user '%s' (user is attached through owner)", id),
					fmt.Sprintf("/data/%d", index))
			}
		}
	}

	rows, err := s.store.ListLicenseUserRows(ctx, account.ID, license.ID, userIDs)
	if err != nil {
		return fmt.Errorf("licenses: detach users: %w", err)
	}

	if len(rows) != len(userIDs) {
		attached := map[uuid.UUID]bool{}
		for _, row := range rows {
			attached[row.UserID] = true
		}

		for index, id := range userIDs {
			if !attached[id] {
				return apperr.UnprocessableNoCode(
					fmt.Sprintf("cannot detach user '%s' (user is not attached)", id),
					fmt.Sprintf("/data/%d", index))
			}
		}
	}

	return s.write(ctx, func(ctx context.Context) error {
		if _, err := s.store.DeleteLicenseUsers(ctx, account.ID, license.ID, userIDs); err != nil {
			return fmt.Errorf("licenses: detach users: %w", err)
		}

		count := license.LicenseUsersCount - int32(len(userIDs)) //nolint:gosec // the count is non-negative
		if count < 0 {
			count = 0
		}

		if err := s.store.SetLicenseUsersCount(ctx, account.ID, license.ID, count); err != nil {
			return fmt.Errorf("licenses: detach users count: %w", err)
		}

		return s.publish(ctx, "license.users.detached", rows)
	})
}

// Entitlements answers the union of the policy- and license-attached
// entitlements.
func (s Scenario) Entitlements(
	ctx context.Context, account domain.Account, idOrKey string, params paging.Params,
) ([]domain.Entitlement, int, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return nil, 0, err
	}

	if err := s.entitlementRelationship(ctx, account, license); err != nil {
		return nil, 0, err
	}

	items, err := s.entlist.ListLicenseEntitlementsOf(ctx, account.ID, license.PolicyID, license.ID, params.Window())
	if err != nil {
		return nil, 0, fmt.Errorf("licenses: entitlements: %w", err)
	}

	count := len(items)
	if params.Mode == paging.ModeOffset {
		count, err = s.entlist.CountLicenseEntitlementsOfUnion(ctx, account.ID, license.PolicyID, license.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("licenses: entitlements count: %w", err)
		}
	}

	return items, count, nil
}

// Entitlement answers one of the license's entitlements.
func (s Scenario) Entitlement(
	ctx context.Context, account domain.Account, idOrKey string, entitlementID uuid.UUID,
) (domain.Entitlement, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Entitlement{}, err
	}

	if err := s.entitlementRelationship(ctx, account, license); err != nil {
		return domain.Entitlement{}, err
	}

	entitlement, err := s.entlist.LicenseEntitlementOf(ctx, account.ID, license.PolicyID, license.ID, entitlementID)
	if err != nil {
		return domain.Entitlement{}, apperr.NotFound("entitlement", entitlementID.String())
	}

	return entitlement, nil
}

func (s Scenario) entitlementRelationship(ctx context.Context, account domain.Account, license domain.License) error {
	if err := access.Require(ctx, "entitlement.read"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}

// AttachEntitlements attaches entitlements, answering the join rows.
func (s Scenario) AttachEntitlements(
	ctx context.Context, account domain.Account, idOrKey string, entitlementIDs []uuid.UUID,
) ([]domain.LicenseEntitlement, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return nil, err
	}

	if err := s.entitlementsWrite(ctx, license); err != nil {
		return nil, err
	}

	for _, id := range entitlementIDs {
		if _, err := s.entitlements.EntitlementByID(ctx, account.ID, id); err != nil {
			return nil, apperr.Validation("ENTITLEMENT_NOT_FOUND", "must exist",
				"/data/relationships/entitlement",
				"license-entitlements/#license-entitlements-object-relationships-entitlement")
		}

		attached, err := s.store.CountLicenseEntitlementsOf(ctx, account.ID, license.ID, []uuid.UUID{id})
		if err != nil {
			return nil, fmt.Errorf("licenses: attach entitlements: %w", err)
		}

		if attached > 0 {
			return nil, apperr.Validation("ENTITLEMENT_TAKEN", "already exists",
				"/data/relationships/entitlement",
				"license-entitlements/#license-entitlements-object-relationships-entitlement")
		}

		viaPolicy, err := s.store.CountPolicyEntitlementsOf(ctx, account.ID, license.PolicyID, []uuid.UUID{id})
		if err != nil {
			return nil, fmt.Errorf("licenses: attach entitlements: %w", err)
		}

		if viaPolicy > 0 {
			return nil, apperr.Validation("ENTITLEMENT_CONFLICT",
				"already exists (entitlement is attached through policy)",
				"/data/relationships/entitlement",
				"license-entitlements/#license-entitlements-object-relationships-entitlement")
		}
	}

	var rows []domain.LicenseEntitlement

	err = s.write(ctx, func(ctx context.Context) error {
		if err := s.store.InsertLicenseEntitlements(ctx, uuid.New(), account.ID, license.ID, entitlementIDs); err != nil {
			return fmt.Errorf("licenses: attach entitlements: %w", err)
		}

		rows, err = s.store.ListLicenseEntitlementRows(ctx, account.ID, license.ID, entitlementIDs)
		if err != nil {
			return fmt.Errorf("licenses: attached entitlements: %w", err)
		}

		return s.publish(ctx, "license.entitlements.attached", rows)
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // the boundary's own error
	}

	return rows, nil
}

// DetachEntitlements detaches entitlements; policy-attached and unknown
// ones are rejected with the reference's errors.
func (s Scenario) DetachEntitlements(
	ctx context.Context, account domain.Account, idOrKey string, entitlementIDs []uuid.UUID,
) error {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return err
	}

	if err := s.entitlementsWrite(ctx, license); err != nil {
		return err
	}

	for index, id := range entitlementIDs {
		viaPolicy, err := s.store.CountPolicyEntitlementsOf(ctx, account.ID, license.PolicyID, []uuid.UUID{id})
		if err != nil {
			return fmt.Errorf("licenses: detach entitlements: %w", err)
		}

		if viaPolicy > 0 {
			return apperr.ForbiddenIndex(
				fmt.Sprintf("cannot detach entitlement '%s' (entitlement is attached through policy)", id),
				fmt.Sprintf("/data/%d", index))
		}

		attached, err := s.store.CountLicenseEntitlementsOf(ctx, account.ID, license.ID, []uuid.UUID{id})
		if err != nil {
			return fmt.Errorf("licenses: detach entitlements: %w", err)
		}

		if attached == 0 {
			return apperr.UnprocessableNoCode(
				fmt.Sprintf("cannot detach entitlement '%s' (entitlement is not attached)", id),
				fmt.Sprintf("/data/%d", index))
		}
	}

	var rows []domain.LicenseEntitlement

	return s.write(ctx, func(ctx context.Context) error {
		rows, err = s.store.ListLicenseEntitlementRows(ctx, account.ID, license.ID, entitlementIDs)
		if err != nil {
			return fmt.Errorf("licenses: detached entitlements: %w", err)
		}

		if _, err := s.store.DeleteLicenseEntitlements(ctx, account.ID, license.ID, entitlementIDs); err != nil {
			return fmt.Errorf("licenses: detach entitlements: %w", err)
		}

		return s.publish(ctx, "license.entitlements.detached", rows)
	})
}

func (s Scenario) entitlementsWrite(ctx context.Context, license domain.License) error {
	if err := access.Require(ctx, "license.entitlements.attach"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseEntitlementsAttach(access.PrincipalOf(ctx), license); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}

// UserActive answers the user's computed status (the license owner
// show renders it).
func (s Scenario) UserActive(ctx context.Context, account domain.Account, userID uuid.UUID) (string, error) {
	user, err := s.users.UserByID(ctx, account.ID, userID)
	if err != nil {
		return "", err //nolint:wrapcheck // the port's own error
	}

	active, err := s.users.UserHasActiveLicense(ctx, account.ID, userID)
	if err != nil {
		return "", fmt.Errorf("licenses: user status: %w", err)
	}

	return user.Status(active, s.now()), nil
}
