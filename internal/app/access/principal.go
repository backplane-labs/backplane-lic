package access

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Bearer kinds (the wire resource types of the bearers).
const (
	BearerUser    = "users"
	BearerProduct = "products"
	BearerLicense = "licenses"
)

// ReasonAllResources is the policy-deny reason appended to the 403 detail
// when a permission is missing.
const ReasonAllResources = " (ensure the token or license is allowed to access all resources)"

type principalKey struct{}

// Principal is the authenticated caller: what every scenario authorizes
// against.
type Principal struct {
	// BearerType is the wire type: users, products or licenses.
	BearerType string
	BearerID   uuid.UUID
	Role       domain.Role
	// TokenID is zero for password (and license-key) authentication.
	TokenID uuid.UUID
	// Permissions is the effective set (role defaults, or the token
	// intersection for token authentication).
	Permissions map[string]struct{}
	// License carries the authenticated license for license-key auth.
	License *domain.LicenseAuth
}

// IsUser/IsProduct/IsLicense are the bearer-kind checks.
func (p Principal) IsUser() bool       { return p.BearerType == BearerUser }
func (p Principal) IsProduct() bool    { return p.BearerType == BearerProduct }
func (p Principal) IsLicense() bool    { return p.BearerType == BearerLicense }
func (p Principal) IsTokenAuthn() bool { return p.TokenID != uuid.Nil }

// Allows reports whether the principal holds the permission.
func (p Principal) Allows(permission string) bool {
	_, ok := p.Permissions[permission]

	return ok
}

// With stores the principal in the context.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalOf loads the principal from the context; the zero value when
// the request is anonymous.
func PrincipalOf(ctx context.Context) Principal {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p
	}

	return Principal{}
}

// Require authorizes the permission for the context's principal. The
// returned error is the 403 document with the reference's reason: the
// bearer's role lacking the permission ("<role> lacks permission to
// perform action"), or — for token authentication whose restrictions
// removed it — "token lacks permission to perform action".
func Require(ctx context.Context, permission string) error {
	p := PrincipalOf(ctx)
	if p.Allows(permission) {
		return nil
	}

	if _, held := RolePermissions(p.Role)[permission]; !held {
		return apperr.ForbiddenReason(ReasonLacksPermission(p.Role))
	}

	return apperr.ForbiddenReason(ReasonTokenLacksPermission)
}

// ReasonLacksPermission is the verify_permissions! reason when the
// bearer's role itself does not hold the permission.
func ReasonLacksPermission(role domain.Role) string {
	return " (" + Whatami(Principal{Role: role}) + " lacks permission to perform action)"
}

// ReasonTokenLacksPermission is the verify_permissions! reason when the
// bearer's role holds the permission but the token's restrictions
// removed it.
const ReasonTokenLacksPermission = " (token lacks permission to perform action)" //nolint:gosec,lll // a policy reason, not a credential

// Whatami names the bearer like the reference's policy whatami (the
// humanized role: user, admin, developer, read only, sales agent,
// support agent, product, license).
func Whatami(p Principal) string {
	switch p.Role {
	case domain.RoleReadOnly:
		return "read only"
	case domain.RoleSalesAgent:
		return "sales agent"
	case domain.RoleSupportRole:
		return "support agent"
	case domain.RoleUser:
		return "user"
	case domain.RoleAdmin:
		return "admin"
	case domain.RoleDeveloper:
		return "developer"
	case domain.RoleProduct:
		return "product"
	case domain.RoleLicense:
		return "license"
	default:
		return "bearer"
	}
}

// RequireBearerKind authorizes the bearer kinds (any of kinds).
func RequireBearerKind(ctx context.Context, kinds ...string) error {
	p := PrincipalOf(ctx)
	for _, k := range kinds {
		if p.BearerType == k {
			return nil
		}
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// BearerHeader answers the Keygen-Bearer response header value.
func (p Principal) BearerHeader() string {
	return p.BearerID.String()
}
