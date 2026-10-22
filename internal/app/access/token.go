// Token resource authorization (TokenPolicy).
package access

import (
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The stored polymorphic bearer types.
const (
	bearerTypeUser    = "User"
	bearerTypeLicense = "License"
)

// TokenScopeKind selects the index scope of the tokens relation for the
// principal ("" = every record).
func TokenScopeKind(p Principal) string {
	switch {
	case isAdminFamily(p):
		return ""
	case p.IsProduct():
		return "product"
	case p.IsLicense():
		return "license"
	case p.IsUser() && p.Role == domain.RoleUser:
		return "user"
	default:
		return "none"
	}
}

// TokenShow authorizes one token: the admin family sees everything; a
// product bearer its users' and its licenses' tokens; everyone else
// only their own.
func TokenShow(p Principal, token domain.Token, userOfProduct, licenseOfProduct func() bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.IsProduct():
		if token.BearerType == bearerTypeUser && userOfProduct() {
			return nil
		}

		if token.BearerType == bearerTypeLicense && licenseOfProduct() {
			return nil
		}
	default:
		if token.BearerType == wireBearerType(p.BearerType) && token.BearerID == p.BearerID {
			return nil
		}
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// TokenRegenerate authorizes regeneration: admin and developer for any
// bearer; a product bearer its users'/licenses' tokens; everyone else
// their own.
func TokenRegenerate(p Principal, token domain.Token, userOfProduct, licenseOfProduct func() bool) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		if p.IsUser() {
			return nil
		}
	case p.IsProduct():
		if token.BearerType == bearerTypeUser && userOfProduct() {
			return nil
		}

		if token.BearerType == bearerTypeLicense && licenseOfProduct() {
			return nil
		}
	default:
		if token.BearerType == wireBearerType(p.BearerType) && token.BearerID == p.BearerID {
			return nil
		}
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// TokenRevoke authorizes revocation: admin, developer for any bearer; a
// product bearer its users'/licenses' tokens; everyone else their own.
func TokenRevoke(p Principal, token domain.Token, userOfProduct, licenseOfProduct func() bool) error {
	return TokenRegenerate(p, token, userOfProduct, licenseOfProduct)
}

// TokenGenerateForUser authorizes minting a token for the target user
// (the /users/{id}/tokens route): admin, developer (any user), product
// (a user licensed to it); user and license bearers are denied.
func TokenGenerateForUser(p Principal, _ domain.User, licensedTo func() bool) error {
	if !p.Allows("user.tokens.generate") {
		return apperr.ForbiddenReason(ReasonLacksPermission(p.Role))
	}

	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		if p.IsUser() {
			return nil
		}
	case p.IsProduct():
		if licensedTo() {
			return nil
		}
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// wireBearerType maps an access bearer kind to the stored polymorphic
// type.
func wireBearerType(kind string) string {
	switch kind {
	case BearerUser:
		return bearerTypeUser
	case BearerProduct:
		return "Product"
	case BearerLicense:
		return bearerTypeLicense
	default:
		return ""
	}
}
