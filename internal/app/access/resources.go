// Package access: the per-resource ownership rules, transcribed from
// the reference's ApplicationPolicy subclasses (app/policies/*.rb).
// Every function answers the plain policy deny (the 403 with the
// "ensure the token or license is allowed to access all resources"
// reason); the permission gate (access.Require) runs before them.
package access

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// deny is the plain deny! error (no inline reason).
func deny() error { return apperr.ForbiddenReason(ReasonAllResources) }

// isAdminFamily answers the admin-side roles that pass every read and
// most write guards: admin, developer, sales_agent, support_agent and
// read_only (the reference's Role(:admin | :developer | :sales_agent |
// :support_agent | :read_only)).
func isAdminFamily(p Principal) bool {
	switch p.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent, domain.RoleSupportRole, domain.RoleReadOnly:
		return true
	default:
		return false
	}
}

// isWriteFamily answers admin and developer only (the write guards
// that exclude sales/support/read-only).
func isWriteFamily(p Principal) bool {
	return p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper
}

// --- products ---

// ProductShow mirrors ProductPolicy#show?: the admin family always;
// the product bearer itself; a user the product when open or her own
// licensed one; a license the product when open or its own.
func ProductShow(p Principal, product domain.Product, licensedToBearer bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && product.ID == p.BearerID:
		return nil
	case (p.Role == domain.RoleUser || p.Role == domain.RoleLicense) &&
		(product.Open() || product.Licensed() && licensedToBearer):
		return nil
	default:
		return deny()
	}
}

// ProductWrite mirrors ProductPolicy#create?/update?/destroy? (the
// update admits the product bearer itself).
func ProductWrite(p Principal, product domain.Product) error {
	switch {
	case isWriteFamily(p):
		return nil
	case p.Role == domain.RoleProduct && product.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// ProductIndex mirrors ProductPolicy#index?: the admin family always;
// user and license bearers pass only their licensed/open post-checks
// (the scoped queries already narrow theirs). The product bearer has
// no index clause at all — it is denied even its own row.
func ProductIndex(p Principal, allOpenOrLicensed bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleUser, p.Role == domain.RoleLicense:
		if !allOpenOrLicensed {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// ProductRelationship mirrors the products/*_policy.rb index/show
// guards (policies, licenses, machines, users): the admin family, or
// the product itself.
func ProductRelationship(p Principal, product domain.Product) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && product.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// ProductTokenCreate mirrors Products::TokenPolicy#create?: the admin
// family's writers only.
func ProductTokenCreate(p Principal) error {
	if isWriteFamily(p) {
		return nil
	}

	return deny()
}

// ProductTokenIndex mirrors Products::TokenPolicy#index?: the admin
// family, and — the reference's quirk — the product bearer for its own
// product.
func ProductTokenIndex(p Principal, product domain.Product) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && product.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// ProductTokenShow mirrors Products::TokenPolicy#show?: the admin
// family, or the product bearer reading its own token.
func ProductTokenShow(p Principal, product domain.Product, tokenBearerID uuid.UUID) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && product.ID == p.BearerID && tokenBearerID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// --- policies ---

// PolicyShow mirrors PolicyPolicy#show?: the admin family always; the
// product bearer its own; a user hers; a license its own.
func PolicyShow(p Principal, policy domain.Policy, ownedByUser, ownedByLicense bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && policy.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && ownedByUser:
		return nil
	case p.Role == domain.RoleLicense && ownedByLicense:
		return nil
	default:
		return deny()
	}
}

// PolicyIndexPostCheck mirrors PolicyPolicy#index?'s post-pagination
// checks over the loaded page: the admin family always; a product
// bearer only pages of its own policies; a license only pages holding
// just its own policy; a user only pages of her policies.
func PolicyIndexPostCheck(p Principal, page []domain.Policy, userPolicies map[uuid.UUID]struct{}, licensePolicyID uuid.UUID) error { //nolint:lll // the wire name is long
	owns := func(id uuid.UUID) bool { _, ok := userPolicies[id]; return ok }

	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct:
		for _, policy := range page { //nolint:gocritic // mirrors the reference's shape
			if policy.ProductID != p.BearerID {
				return deny()
			}
		}

		return nil
	case p.Role == domain.RoleLicense:
		for _, policy := range page { //nolint:gocritic // mirrors the reference's shape
			if policy.ID != licensePolicyID {
				return deny()
			}
		}

		return nil
	case p.Role == domain.RoleUser:
		for _, policy := range page { //nolint:gocritic // mirrors the reference's shape
			if !owns(policy.ID) {
				return deny()
			}
		}

		return nil
	default:
		return deny()
	}
}

// PolicyWrite mirrors PolicyPolicy#create?/update?/destroy? (sales
// agents may create and update but not destroy).
func PolicyWrite(p Principal, policy domain.Policy, destroy bool) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		return nil
	case p.Role == domain.RoleSalesAgent && !destroy:
		return nil
	case p.Role == domain.RoleProduct && policy.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// PolicyRelationship mirrors the policies/*_policy.rb index/show
// guards over a policy's sub-resources (product, licenses, pool,
// entitlements): the admin family, or the product owning the policy.
func PolicyRelationship(p Principal, policy domain.Policy) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && policy.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// PolicyEntitlementsWrite mirrors Policies::EntitlementPolicy#attach?/
// detach?: admin, developer, sales agent, or the owning product.
func PolicyEntitlementsWrite(p Principal, policy domain.Policy) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && policy.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// PolicyPoolPop mirrors Policies::PoolPolicy#pop?.
func PolicyPoolPop(p Principal, policy domain.Policy) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		return nil
	case p.Role == domain.RoleProduct && policy.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// PolicyPoolPostCheck mirrors PoolPolicy#index?'s checks: the admin
// family always; a product bearer only pages of its own keys.
func PolicyPoolPostCheck(p Principal, policy domain.Policy, page []domain.Key) error {
	if isAdminFamily(p) {
		return nil
	}

	if p.Role != domain.RoleProduct || policy.ProductID != p.BearerID {
		return deny()
	}

	for _, key := range page { //nolint:gocritic // mirrors the reference's shape
		if key.ProductID != p.BearerID {
			return deny()
		}
	}

	return nil
}

// --- entitlements ---

// EntitlementShow mirrors EntitlementPolicy#show? (and the index
// post-check over a page): the admin family and the product bearer see
// everything; user and license bearers only their own.
func EntitlementShow(p Principal, id uuid.UUID, entitlements map[uuid.UUID]struct{}) error {
	switch {
	case isAdminFamily(p) || p.Role == domain.RoleProduct:
		return nil
	case p.Role == domain.RoleUser || p.Role == domain.RoleLicense:
		if _, ok := entitlements[id]; !ok {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// EntitlementWrite mirrors EntitlementPolicy#create?/update?/destroy?.
func EntitlementWrite(p Principal) error {
	if p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper {
		return nil
	}

	return deny()
}

// --- keys ---

// KeyShow mirrors KeyPolicy#show?.
func KeyShow(p Principal, key domain.Key) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && key.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// KeyIndexPostCheck mirrors KeyPolicy#index?'s product post-check.
func KeyIndexPostCheck(p Principal, page []domain.Key) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct:
		for _, key := range page { //nolint:gocritic // mirrors the reference's shape
			if key.ProductID != p.BearerID {
				return deny()
			}
		}

		return nil
	default:
		return deny()
	}
}

// KeyWrite mirrors KeyPolicy#create?/update?/destroy?.
func KeyWrite(p Principal, key domain.Key) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		return nil
	case p.Role == domain.RoleProduct && key.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// KeyRelationship mirrors Keys::{Product,Policy}Policy#show?: the
// admin family, or the product owning the key's policy.
func KeyRelationship(p Principal, key domain.Key) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && key.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}
