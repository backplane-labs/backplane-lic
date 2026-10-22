// Second-factor resource authorization (Users::SecondFactorPolicy).
package access

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// SecondFactorShow authorizes reading the user's second factors: an
// admin sees everyone's; the other admin-family roles, products and
// plain users only their own.
func SecondFactorShow(p Principal, userID uuid.UUID) error {
	switch {
	case p.Role == domain.RoleAdmin && p.IsUser():
		return nil
	case (isAdminFamily(p) || p.IsProduct()) && p.BearerID == userID:
		return nil
	case p.IsUser() && p.BearerID == userID:
		return nil
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// SecondFactorWrite authorizes create/update/destroy: like Show, but
// read-only bearers are always denied.
func SecondFactorWrite(p Principal, userID uuid.UUID) error {
	if p.Role == domain.RoleReadOnly {
		return apperr.ForbiddenReason(" (" + Whatami(p) + " lacks permission to perform action)")
	}

	return SecondFactorShow(p, userID)
}
