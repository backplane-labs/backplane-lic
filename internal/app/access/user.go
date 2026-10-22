// User resource authorization: the UserPolicy rules, expressed over the
// principal. Permission pre-checks (user.read etc.) run separately via
// Require.
package access

import (
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ReasonNoCredential is the policy-deny reason of an anonymous bearer.
const ReasonNoCredential = " (ensure a token or license is present and valid)"

// ReasonMustHaveUserRole is the ban/unban deny reason.
const ReasonMustHaveUserRole = " (must have a user role)"

// The role privilege order (Role#<=>).
const (
	rankAdmin      = 7
	rankDeveloper  = 5
	rankSalesAgent = 3
	rankSupport    = 2
	rankReadOnly   = 1
	rankUser       = 0
)

// RoleRank is the privilege order of the roles (Role#<=>).
func RoleRank(r domain.Role) int {
	switch r {
	case domain.RoleAdmin:
		return rankAdmin
	case domain.RoleDeveloper:
		return rankDeveloper
	case domain.RoleSalesAgent:
		return rankSalesAgent
	case domain.RoleSupportRole:
		return rankSupport
	case domain.RoleReadOnly:
		return rankReadOnly
	default:
		return rankUser
	}
}

// The index scope kinds of the users relation.
const (
	scopeKindProduct = "product"
	scopeKindLicense = "license"
	scopeKindUser    = "user"
	scopeKindNone    = "none"
)

// UserScopeKind selects the index scope of the users relation for the
// principal ("" = every record).
func UserScopeKind(p Principal) string {
	switch {
	case isAdminFamily(p):
		return ""
	case p.IsProduct():
		return scopeKindProduct
	case p.IsLicense():
		return scopeKindLicense
	case p.IsUser() && p.Role == domain.RoleUser:
		return scopeKindUser
	default:
		return scopeKindNone
	}
}

// UserShow authorizes one record: admin-family sees everything; a
// product bearer plain-user records; a user bearer self and teammates;
// a license bearer its owner and attached users. The user.read
// permission check runs in the scenario before this.
func UserShow(p Principal, user domain.User, teammate, licenseUser func() bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.IsProduct():
		if user.Role == domain.RoleUser {
			return nil
		}
	case p.IsLicense():
		if licenseUser() {
			return nil
		}
	case p.IsUser() && (user.ID == p.BearerID || teammate()):
		return nil
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// UserCreate authorizes creating the record (privilege escalation
// included).
func UserCreate(p Principal, accountProtected bool, role domain.Role) error {
	allowed := false

	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		allowed = p.IsUser()
	case p.IsProduct() && role == domain.RoleUser:
		allowed = true
	case p.BearerType == "" && !accountProtected:
		allowed = true
	}

	if !allowed {
		reason := ReasonAllResources
		if p.BearerType == "" {
			reason = ReasonNoCredential
		}

		return apperr.ForbiddenReason(reason)
	}

	return verifyPrivileges(p, role)
}

// UserUpdate authorizes mutating the record.
func UserUpdate(p Principal, user domain.User, role domain.Role) error {
	allowed := false

	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		allowed = p.IsUser()
	case p.IsProduct() && user.Role == domain.RoleUser:
		allowed = true
	case p.IsUser() && user.ID == p.BearerID:
		allowed = true
	}

	if !allowed {
		return apperr.ForbiddenReason(ReasonAllResources)
	}

	return verifyPrivileges(p, role)
}

// UserDestroy authorizes deleting the record.
func UserDestroy(p Principal, user domain.User) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		if p.IsUser() {
			return nil
		}
	case p.IsProduct() && user.Role == domain.RoleUser:
		return nil
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// UserBan authorizes ban/unban: only plain-user targets are bannable.
func UserBan(p Principal, user domain.User) error {
	if user.Role != domain.RoleUser {
		return apperr.ForbiddenReason(ReasonMustHaveUserRole)
	}

	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent || p.Role == domain.RoleSupportRole:
		if p.IsUser() {
			return nil
		}
	case p.IsProduct():
		return nil
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// UserPasswordUpdate authorizes the update-password action: only the
// bearer themself.
func UserPasswordUpdate(p Principal, user domain.User) error {
	if p.IsUser() && user.ID == p.BearerID {
		return nil
	}

	if isAdminFamily(p) {
		// An admin touching another user's password: the policy denies.
		return apperr.ForbiddenReason(ReasonAllResources)
	}

	return apperr.NotFound("user", user.ID.String())
}

// verifyPrivileges mirrors UserPolicy#verify_privileges: the bearer may
// not assign a role above their own (anonymous may only create
// plain users, checked by the caller).
func verifyPrivileges(p Principal, role domain.Role) error {
	if p.BearerType != "" && RoleRank(p.Role) < RoleRank(role) {
		return apperr.ForbiddenReason(ReasonAllResources)
	}

	return nil
}
