// License ownership rules, transcribed from LicensePolicy and the
// Licenses::* policies (app/policies/license_policy.rb,
// app/policies/licenses/*).
package access

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// --- licenses ---

// LicenseIndexPostCheck mirrors LicensePolicy#index?: the admin family
// always; a product bearer only pages of its own; a user only pages
// she owns or is attached to (attached lists her licensee licenses);
// the license bearer only pages holding itself.
func LicenseIndexPostCheck(p Principal, page []domain.License, attached map[uuid.UUID]bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct:
		for i := range page {
			if page[i].ProductID != p.BearerID {
				return deny()
			}
		}

		return nil
	case p.Role == domain.RoleUser:
		for i := range page {
			if !LicenseVisibleToUser(p, page[i], attached[page[i].ID]) {
				return deny()
			}
		}

		return nil
	default:
		// LicensePolicy#index? has no license branch: the license bearer
		// itself cannot index.
		return deny()
	}
}

// LicenseVisibleToUser answers whether the user owns or is attached to
// the license (bearer.license_ids covers owner ∪ licensees).
func LicenseVisibleToUser(p Principal, license domain.License, userAttached bool) bool {
	return license.OwnerID.Valid && license.OwnerID.UUID == p.BearerID || userAttached
}

// LicenseShow mirrors LicensePolicy#show?.
func LicenseShow(p Principal, license domain.License, userAttached bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && LicenseVisibleToUser(p, license, userAttached):
		return nil
	case p.Role == domain.RoleLicense && license.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseCreate mirrors LicensePolicy#create?.
func LicenseCreate(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID {
			return deny()
		}

		if license.Policy.Protected != nil && *license.Policy.Protected {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// LicenseUpdate mirrors LicensePolicy#update? (users and licenses
// cannot update at all).
func LicenseUpdate(p Principal, license domain.License) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseDestroy mirrors LicensePolicy#destroy?.
func LicenseDestroy(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID {
			return deny()
		}

		if license.Policy.Protected != nil && *license.Policy.Protected {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// LicenseAction is the shared record clause of the acting routes
// (revoke, renew, check-in, check-out, validate, suspend, reinstate,
// usage): admin family / own product; revoke, renew and check-in
// additionally admit the owner of an unprotected license, check-out
// and validate also licensees and the license itself, usage.increment
// also the license bearer.
func LicenseAction(p Principal, license domain.License, clause string, userAttached bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && LicenseVisibleToUser(p, license, userAttached):
		return licenseUserClause(p, license, clause)
	case p.Role == domain.RoleLicense && license.ID == p.BearerID:
		return licenseBearerClause(clause)
	default:
		return deny()
	}
}

func licenseUserClause(p Principal, license domain.License, clause string) error {
	switch clause {
	case "check-out", "validate":
		return nil
	case "revoke", "renew", "check-in", "usage.increment":
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID {
			return deny()
		}

		if license.ProtectedFlag() {
			return deny()
		}

		return nil
	default:
		// suspend, reinstate, usage.decrement, usage.reset.
		return deny()
	}
}

func licenseBearerClause(clause string) error {
	switch clause {
	case "check-out", "check-in", "validate", "usage.increment":
		return nil
	default:
		return deny()
	}
}

// --- relationships (Licenses::* policies) ---

// LicenseRelationshipShow mirrors the index/show guards of the
// licenses sub-policies (machines, users, entitlements, group, owner,
// policy, product): the admin family, the owning product, the owner or
// an attached user, or the license itself.
func LicenseRelationshipShow(p Principal, license domain.License, userAttached bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && LicenseVisibleToUser(p, license, userAttached):
		return nil
	case p.Role == domain.RoleLicense && license.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseTokenIndex mirrors Licenses::TokenPolicy#index?/show?: the
// admin family or the owning product (never a user or the license).
func LicenseTokenIndex(p Principal, license domain.License) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseTokenCreate mirrors Licenses::TokenPolicy#create?: admin,
// developer, sales agent or the owning product.
func LicenseTokenCreate(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseGroupUpdate mirrors Licenses::GroupPolicy#update?: admin,
// developer, sales agent or the owning product.
func LicenseGroupUpdate(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseOwnerUpdate mirrors Licenses::OwnerPolicy#update?: admin,
// developer, sales agent or the owning product.
func LicenseOwnerUpdate(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicensePolicyUpdate mirrors Licenses::PolicyPolicy#update?: admin,
// developer or sales agent; the owning product when the target policy
// is its own; the owner of an unprotected license to an unprotected
// policy.
func LicensePolicyUpdate(p Principal, license domain.License, target domain.Policy) error {
	switch p.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent:
		return nil
	case domain.RoleProduct:
		if license.ProductID != p.BearerID || target.ProductID != p.BearerID {
			return deny()
		}

		return nil
	case domain.RoleUser:
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID {
			return deny()
		}

		if license.ProtectedFlag() || target.Protected != nil && *target.Protected {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// LicenseUsersAttach mirrors Licenses::UserPolicy#attach?/detach?: the
// admin family, the owning product, or the owner of an unprotected
// license.
func LicenseUsersAttach(p Principal, license domain.License) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID {
			return deny()
		}

		if license.ProtectedFlag() {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// LicenseEntitlementsAttach mirrors Licenses::EntitlementPolicy#attach?/
// detach?: admin, developer, sales agent or the owning product.
func LicenseEntitlementsAttach(p Principal, license domain.License) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// LicenseUsageWrite mirrors Licenses::UsagePolicy: increment admits
// the owner of an unprotected license and the license itself;
// decrement/reset only the admin family and the owning product.
func LicenseUsageWrite(p Principal, license domain.License, action string) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && license.ProductID == p.BearerID:
		return nil
	case action == "increment" && p.Role == domain.RoleUser:
		if !license.OwnerID.Valid || license.OwnerID.UUID != p.BearerID || license.ProtectedFlag() {
			return deny()
		}

		return nil
	case action == "increment" && p.Role == domain.RoleLicense && license.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}
