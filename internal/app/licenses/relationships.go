// License relationships: machines, tokens, product, policy (transfer),
// group, owner (and the v1.5 user alias), users and entitlements.
package licenses

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// MachinePage is the license's machine index page.
type MachinePage struct {
	Items []domain.Machine
	Count int
	Next  uuid.NullUUID
}

// Machines answers the license's machines (the fingerprint, product
// and user filters apply).
func (s Scenario) Machines(
	ctx context.Context, account domain.Account, idOrKey string, f MachineFilter, params paging.Params,
) (MachinePage, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return MachinePage{}, err
	}

	if err := access.Require(ctx, "machine.read"); err != nil {
		return MachinePage{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return MachinePage{}, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return MachinePage{}, err //nolint:wrapcheck // the authorization's own error
	}

	machines, err := s.store.LicenseMachines(ctx, account.ID, license.ID, f, s.now())
	if err != nil {
		return MachinePage{}, fmt.Errorf("licenses: machines: %w", err)
	}

	window := params.Window()
	out := MachinePage{Items: machines, Count: len(machines)}

	if params.Mode == paging.ModeCursor && len(machines) > int(window.Limit) {
		out.Items = machines[:window.Limit]
	}

	return out, nil
}

// Machine answers one of the license's machines by id or fingerprint.
func (s Scenario) Machine(
	ctx context.Context, account domain.Account, idOrKey, machineIDOrFingerprint string,
) (domain.Machine, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := access.Require(ctx, "machine.read"); err != nil {
		return domain.Machine{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.Machine{}, err //nolint:wrapcheck // the authorization's own error
	}

	var (
		machine domain.Machine
		now     = s.now()
	)

	if uuidRe.MatchString(machineIDOrFingerprint) {
		machine, err = s.store.LicenseMachineByID(ctx, account.ID, license.ID,
			uuid.MustParse(machineIDOrFingerprint), now)
	} else {
		machine, err = s.store.LicenseMachineByFingerprint(ctx, account.ID, license.ID,
			machineIDOrFingerprint, now)
	}

	if err != nil {
		return domain.Machine{}, apperr.NotFound("machine", machineIDOrFingerprint)
	}

	return machine, nil
}

// Tokens answers the license's activation tokens.
func (s Scenario) Tokens(
	ctx context.Context, account domain.Account, idOrKey string, params paging.Params,
) (tokens.Page, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return tokens.Page{}, err
	}

	if err := access.LicenseTokenIndex(access.PrincipalOf(ctx), license); err != nil {
		return tokens.Page{}, err //nolint:wrapcheck // the authorization's own error
	}

	return s.tokens.List( //nolint:wrapcheck // the manager's own error
		ctx, account, access.PrincipalOf(ctx), tokens.Filter{
			BearerType: "License", BearerID: uuid.NullUUID{UUID: license.ID, Valid: true},
		}, params)
}

// Token answers one of the license's activation tokens.
func (s Scenario) Token(
	ctx context.Context, account domain.Account, idOrKey string, tokenID uuid.UUID,
) (domain.Token, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Token{}, err
	}

	if err := access.LicenseTokenIndex(access.PrincipalOf(ctx), license); err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the authorization's own error
	}

	token, err := s.tokens.Show(ctx, account, access.PrincipalOf(ctx), tokenID)
	if err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the scenario's own error
	}

	if token.BearerType != "License" || token.BearerID != license.ID {
		return domain.Token{}, apperr.NotFound("token", tokenID.String())
	}

	return token, nil
}

// GenerateToken mints an activation token for the license.
func (s Scenario) GenerateToken(
	ctx context.Context, account domain.Account, idOrKey string, attrs tokens.CreateAttrs,
) (domain.Token, string, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Token{}, "", err
	}

	if err := access.LicenseTokenCreate(access.PrincipalOf(ctx), license); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	return s.tokens.CreateForLicense( //nolint:wrapcheck // the manager's own error
		ctx, account, access.PrincipalOf(ctx), license.ID, attrs,
	)
}

// Product answers the license's product document.
func (s Scenario) Product(ctx context.Context, account domain.Account, idOrKey string) (domain.Product, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.Require(ctx, "product.read"); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	product, err := s.products.ProductByID(ctx, account.ID, license.ProductID)
	if err != nil {
		return domain.Product{}, apperr.NotFound("product", license.ProductID.String())
	}

	return product, nil
}

// Policy answers the license's policy document.
func (s Scenario) Policy(ctx context.Context, account domain.Account, idOrKey string) (domain.Policy, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Policy{}, err
	}

	if err := access.Require(ctx, "policy.read"); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.Policy{}, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	return license.Policy, nil
}

// Transfer moves the license to another policy (PATCH policy).
func (s Scenario) Transfer(
	ctx context.Context, account domain.Account, idOrKey string, targetID uuid.UUID,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	target, err := s.policies.PolicyByID(ctx, account.ID, targetID)
	if err != nil {
		return domain.License{}, apperr.Validation("POLICY_NOT_FOUND", "must exist",
			"/data/relationships/policy", "licenses/#licenses-object-relationships-policy")
	}

	if err := access.Require(ctx, "license.policy.update"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.License{}, err
	}

	_ = attached

	if err := access.LicensePolicyUpdate(access.PrincipalOf(ctx), license, target); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if detail, bad := transferConflict(license.Policy, target); bad {
		return domain.License{}, apperr.Validation("POLICY_NOT_COMPATIBLE", detail,
			"/data/relationships/policy", "licenses/#licenses-object-relationships-policy")
	}

	now := s.now()
	expiry := license.Expiry

	// The reset strategy recomputes the expiry from the new policy.
	if target.TransferStrategy == "RESET_EXPIRY" {
		expiry = nil

		if target.Duration != nil {
			t := now.Add(durationOf(target.Duration))
			expiry = &t
		}
	}

	var out domain.License

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.TransferLicense(ctx, account.ID, license.ID, target.ID, target.ProductID, expiry, now); err != nil {
			return fmt.Errorf("licenses: transfer: %w", err)
		}

		out, err = s.store.LicenseByID(ctx, account.ID, license.ID)
		if err != nil {
			return fmt.Errorf("licenses: transfer reload: %w", err)
		}

		s.publish(ctx, "license.policy.updated", out)

		return nil
	}); err != nil {
		return domain.License{}, err
	}

	return out, nil
}

// transferConflict answers the incompatible-transfer detail.
func transferConflict(prev, next domain.Policy) (string, bool) {
	switch {
	case next.Encrypted != prev.Encrypted:
		return "cannot change from an encrypted policy to an unencrypted policy (or vice-versa)", true
	case next.UsePool != prev.UsePool:
		return "cannot change from a pooled policy to an unpooled policy (or vice-versa)", true
	case next.Scheme != prev.Scheme:
		return "cannot change to a policy with a different scheme", true
	case uniquenessRank(next.MachineUniquenessStrategy) > uniquenessRank(prev.MachineUniquenessStrategy):
		return "cannot change to a policy with a more strict machine uniqueness strategy", true
	case uniquenessRank(next.ComponentUniquenessStrategy) > uniquenessRank(prev.ComponentUniquenessStrategy):
		return "cannot change to a policy with a more strict component uniqueness strategy", true
	default:
		return "", false
	}
}

func uniquenessRank(strategy string) int {
	switch strategy {
	case "UNIQUE_PER_ACCOUNT":
		return 4 //nolint:mnd // the reference's literal rank
	case "UNIQUE_PER_PRODUCT":
		return 3 //nolint:mnd // the reference's literal rank
	case "UNIQUE_PER_POLICY":
		return 2 //nolint:mnd // the reference's literal rank
	case "UNIQUE_PER_LICENSE":
		return 1
	default:
		return 0
	}
}

// Group answers the license's group (404 when none).
func (s Scenario) Group(ctx context.Context, account domain.Account, idOrKey string) (domain.Group, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.Group{}, err
	}

	if err := access.Require(ctx, "group.read"); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.Group{}, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the authorization's own error
	}

	if !license.GroupID.Valid {
		return domain.Group{}, &apperr.T{
			Status: 404, //nolint:mnd // the reference's literal status
			Title:  apperr.TitleNotFound,
			Detail: "The requested group was not found",
			Code:   "NOT_FOUND",
		}
	}

	group, err := s.groups.GroupByID(ctx, account.ID, license.GroupID.UUID)
	if err != nil {
		return domain.Group{}, &apperr.T{
			Status: 404, //nolint:mnd // the reference's literal status
			Title:  apperr.TitleNotFound,
			Detail: "The requested group was not found",
			Code:   "NOT_FOUND",
		}
	}

	return group, nil
}

// SetGroup writes the license's group (PATCH group); GroupSet false
// clears it (data: null).
func (s Scenario) SetGroup(
	ctx context.Context, account domain.Account, idOrKey string, group uuid.NullUUID,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if group.Valid {
		if _, err = s.groups.GroupByID(ctx, account.ID, group.UUID); err != nil {
			return domain.License{}, apperr.Validation("GROUP_NOT_FOUND", "must exist",
				"/data/relationships/group", "licenses/#licenses-object-relationships-group")
		}
	}

	if err := access.Require(ctx, "license.group.update"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseGroupUpdate(access.PrincipalOf(ctx), license); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if group.Valid {
		if err := s.checkGroupLimit(ctx, account, license, group.UUID); err != nil {
			return domain.License{}, err
		}
	}

	var out domain.License

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseGroup(ctx, account.ID, license.ID, group, s.now()); err != nil {
			return fmt.Errorf("licenses: set group: %w", err)
		}

		out, err = s.store.LicenseByID(ctx, account.ID, license.ID)
		if err != nil {
			return fmt.Errorf("licenses: set group reload: %w", err)
		}

		s.publish(ctx, "license.group.updated", out)

		return nil
	}); err != nil {
		return domain.License{}, err
	}

	return out, nil
}

// checkGroupLimit runs the group license-limit validation.
func (s Scenario) checkGroupLimit(
	ctx context.Context, account domain.Account, _ domain.License, groupID uuid.UUID,
) error {
	group, err := s.groups.GroupByID(ctx, account.ID, groupID)
	if err != nil || group.MaxLicenses == nil {
		return nil //nolint:nilerr // a missing limit is not an error
	}

	count, err := s.store.CountLicensesOfGroup(ctx, account.ID, groupID)
	if err != nil {
		return fmt.Errorf("licenses: group limit: %w", err)
	}

	if count >= *group.MaxLicenses {
		return apperr.Validation("GROUP_LICENSE_LIMIT_EXCEEDED",
			fmt.Sprintf("license count has exceeded maximum allowed by current group (%d)", *group.MaxLicenses),
			"/data/relationships/group", "licenses/#licenses-object-relationships-group")
	}

	return nil
}

// Owner answers the license's owner (data null when none).
func (s Scenario) Owner(ctx context.Context, account domain.Account, idOrKey string) (domain.User, bool, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.User{}, false, err
	}

	if err := access.Require(ctx, "user.read"); err != nil {
		return domain.User{}, false, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.User{}, false, err
	}

	if err := access.LicenseRelationshipShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.User{}, false, err //nolint:wrapcheck // the authorization's own error
	}

	if !license.OwnerID.Valid {
		return domain.User{}, false, nil
	}

	owner, err := s.users.UserByID(ctx, account.ID, license.OwnerID.UUID)
	if err != nil {
		return domain.User{}, false, apperr.NotFound("user", license.OwnerID.UUID.String())
	}

	return owner, true, nil
}

// SetOwner writes the license's owner (PATCH owner and the v1.5 user
// alias); OwnerSet false clears it.
func (s Scenario) SetOwner( //nolint:cyclop,funlen // the transfer's validations, verbatim
	ctx context.Context, account domain.Account, idOrKey string, owner uuid.NullUUID, v15 bool,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if owner.Valid {
		if _, err = s.users.UserByID(ctx, account.ID, owner.UUID); err != nil {
			return domain.License{}, apperr.Validation("USER_NOT_FOUND", "must exist",
				"/data/relationships/owner", "licenses/#licenses-object-relationships-owner")
		}
	}

	if err := access.Require(ctx, "license.owner.update"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseOwnerUpdate(access.PrincipalOf(ctx), license); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	var user domain.User

	if owner.Valid {
		user, err = s.users.UserByID(ctx, account.ID, owner.UUID)
		if err != nil {
			return domain.License{}, fmt.Errorf("licenses: set owner: %w", err)
		}

		// The owner may not already be a licensee.
		licensee, err := s.store.LicenseeIsUser(ctx, account.ID, license.ID, owner.UUID)
		if err != nil {
			return domain.License{}, fmt.Errorf("licenses: set owner: %w", err)
		}

		if licensee {
			return domain.License{}, apperr.Validation("OWNER_INVALID",
				"already exists (user is attached through users)",
				"/data/relationships/owner", "licenses/#licenses-object-relationships-owner")
		}

		// Users overage according to the policy's strategy.
		next := license.LicenseUsersCount + 1
		if limit := license.MaxUsers(); limit != nil && next > *limit &&
			!overageCovers(license.Policy, float64(next), float64(*limit), !license.Policy.Floating) {
			return domain.License{}, apperr.Validation("USER_LIMIT_EXCEEDED",
				fmt.Sprintf("user count has exceeded maximum allowed for license (%d)", *limit),
				"/data/relationships/users", "licenses/#licenses-object-relationships-users")
		}
	}

	// The license inherits the new owner's group when it has none.
	group := license.GroupID
	if owner.Valid && !license.GroupID.Valid {
		group = user.GroupID
	}

	var out domain.License

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseOwner(ctx, account.ID, license.ID, owner, group, s.now()); err != nil {
			return fmt.Errorf("licenses: set owner: %w", err)
		}

		out, err = s.store.LicenseByID(ctx, account.ID, license.ID)
		if err != nil {
			return fmt.Errorf("licenses: set owner reload: %w", err)
		}

		if v15 {
			s.publish(ctx, "license.user.updated", out)
		} else {
			s.publish(ctx, "license.owner.updated", out)
		}

		return nil
	}); err != nil {
		return domain.License{}, err
	}

	return out, nil
}

// overageCovers answers whether the overage strategy covers the count.
func overageCovers(policy domain.Policy, count, limit float64, nodeLocked bool) bool {
	switch policy.OverageStrategy {
	case "ALWAYS_ALLOW_OVERAGE":
		return true
	case "ALLOW_1_25X_OVERAGE":
		return !nodeLocked && count <= limit*1.25
	case "ALLOW_1_5X_OVERAGE":
		return !nodeLocked && count <= limit*1.5
	case "ALLOW_2X_OVERAGE":
		return !nodeLocked || count <= limit*2
	default:
		return false
	}
}

func durationOf(seconds *int32) time.Duration {
	if seconds == nil {
		return 0
	}

	return time.Duration(*seconds) * time.Second
}
