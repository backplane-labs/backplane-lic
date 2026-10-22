// The nested license routes: the licenses of a policy, product, user
// or group (the parent resources load and authorize in their own
// scenarios; these answer the license page within the parent).
package licenses

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// OfPolicy answers the policy's licenses (the product/user/suspended
// filters apply on top of the policy).
func (s Scenario) OfPolicy(
	ctx context.Context, account domain.Account, policy domain.Policy, f Filter, params paging.Params,
) (List, error) {
	f.PolicyID = uuid.NullUUID{UUID: policy.ID, Valid: true}

	return s.nestedIndex(ctx, account, f, params, "policy.read", "")
}

// OfPolicyShow answers one of the policy's licenses by id or key.
func (s Scenario) OfPolicyShow(
	ctx context.Context, account domain.Account, policy domain.Policy, idOrKey string,
) (domain.License, error) {
	return s.nestedShow(ctx, account, idOrKey, "policy.read", func(license domain.License) bool {
		return license.PolicyID == policy.ID
	})
}

// OfProduct answers the product's licenses.
func (s Scenario) OfProduct(
	ctx context.Context, account domain.Account, product domain.Product, f Filter, params paging.Params,
) (List, error) {
	f.ProductID = uuid.NullUUID{UUID: product.ID, Valid: true}

	return s.nestedIndex(ctx, account, f, params, "license.read", "")
}

// OfProductShow answers one of the product's licenses.
func (s Scenario) OfProductShow(
	ctx context.Context, account domain.Account, product domain.Product, idOrKey string,
) (domain.License, error) {
	return s.nestedShow(ctx, account, idOrKey, "license.read", func(license domain.License) bool {
		return license.ProductID == product.ID
	})
}

// OfUser answers the user's licenses (owner ∪ attached).
func (s Scenario) OfUser(
	ctx context.Context, account domain.Account, userID uuid.UUID, f Filter, params paging.Params,
) (List, error) {
	// The user filter covers owner ∪ licensees.
	f.UserID = uuid.NullUUID{UUID: userID, Valid: true}

	return s.nestedIndex(ctx, account, f, params, "license.read", "")
}

// OfUserShow answers one of the user's licenses.
func (s Scenario) OfUserShow(
	ctx context.Context, account domain.Account, userID uuid.UUID, idOrKey string,
) (domain.License, error) {
	return s.nestedShow(ctx, account, idOrKey, "license.read", func(license domain.License) bool {
		if license.OwnerID.Valid && license.OwnerID.UUID == userID {
			return true
		}

		attached, err := s.store.LicenseeIsUser(ctx, account.ID, license.ID, userID)

		return err == nil && attached
	})
}

// OfGroup answers the group's licenses.
func (s Scenario) OfGroup(
	ctx context.Context, account domain.Account, group domain.Group, f Filter, params paging.Params,
) (List, error) {
	f.GroupID = uuid.NullUUID{UUID: group.ID, Valid: true}

	if access.PrincipalOf(ctx).Role != domain.RoleUser {
		return s.nestedIndex(ctx, account, f, params, "group.licenses.read", "")
	}

	owned, err := s.groups.OwnedGroupIDsOfUser(ctx, account.ID, access.PrincipalOf(ctx).BearerID)
	if err != nil {
		return List{}, fmt.Errorf("licenses: owned groups: %w", err)
	}

	if len(owned) == 0 {
		return List{}, apperr.ForbiddenReason(access.ReasonAllResources)
	}

	if !slices.Contains(owned, group.ID) {
		return List{Items: []domain.License{}}, nil
	}

	return s.nestedIndex(ctx, account, f, params, "group.licenses.read", "group_owner")
}

// OfGroupShow answers one of the group's licenses.
func (s Scenario) OfGroupShow(
	ctx context.Context, account domain.Account, group domain.Group, idOrKey string,
) (domain.License, error) {
	license, err := s.loadRecord(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if !license.GroupID.Valid || license.GroupID.UUID != group.ID {
		return domain.License{}, apperr.NotFound("license", idOrKey)
	}

	if err := access.Require(ctx, "group.licenses.read"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // authorization error
	}

	principal := access.PrincipalOf(ctx)
	groupOwned := false

	if principal.Role == domain.RoleUser {
		owned, err := s.groups.OwnedGroupIDsOfUser(ctx, account.ID, principal.BearerID)
		if err != nil {
			return domain.License{}, fmt.Errorf("licenses: owned groups: %w", err)
		}

		groupOwned = slices.Contains(owned, group.ID)
	}

	if err := access.GroupLicenseShow(principal, license, groupOwned); err != nil {
		return domain.License{}, err //nolint:wrapcheck // authorization error
	}

	return license, nil
}

func (s Scenario) nestedIndex(
	ctx context.Context, account domain.Account, f Filter, params paging.Params, permission, scopeOverride string,
) (List, error) {
	if err := access.Require(ctx, permission); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	scopeKind, scopeID := licenseScope(principal)
	if scopeOverride != "" {
		scopeKind = scopeOverride
	}

	window := params.Window()
	now := s.now()

	items, err := s.store.ListLicenses(ctx, account.ID, f, scopeKind, scopeID, window, now)
	if err != nil {
		return List{}, fmt.Errorf("licenses: nested index: %w", err)
	}

	attached := map[uuid.UUID]bool{}

	if principal.Role == domain.RoleUser && scopeKind != "group_owner" {
		for i := range items {
			var attachErr error

			attached[items[i].ID], attachErr = s.userAttached(ctx, account.ID, principal.BearerID, items[i])
			if attachErr != nil {
				return List{}, attachErr
			}
		}
	}

	// The group-owner route has its own policy, not LicensePolicy#index?.
	if scopeKind != "group_owner" {
		if err := access.LicenseIndexPostCheck(principal, items, attached); err != nil {
			return List{}, err //nolint:wrapcheck // the authorization's own error
		}
	}

	out := List{Items: items, Count: len(items)}

	if params.Mode == paging.ModeOffset {
		count, err := s.store.CountLicenses(ctx, account.ID, f, scopeKind, scopeID, now)
		if err != nil {
			return List{}, fmt.Errorf("licenses: nested index count: %w", err)
		}

		out.Count = count
	}

	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		next := out.Items[len(out.Items)-1].ID
		out.Next = uuid.NullUUID{UUID: next, Valid: true}
	}

	return out, nil
}

func (s Scenario) nestedShow(
	ctx context.Context, account domain.Account, idOrKey, permission string,
	within func(domain.License) bool,
) (domain.License, error) {
	if err := access.Require(ctx, permission); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if !within(license) {
		return domain.License{}, apperr.NotFound("license", idOrKey)
	}

	return license, nil
}
