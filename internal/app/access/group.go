// Group and group-owner resource authorization (GroupPolicy,
// Groups::GroupOwnerPolicy).
package access

import (
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// GroupShow authorizes one group: the admin family and product bearers
// see everything; a user bearer their own group (membership or
// ownership); a license bearer the license's group.
func GroupShow(p Principal, group domain.Group, memberOrOwner func(domain.Group) bool) error {
	switch {
	case isAdminFamily(p), p.IsProduct():
		return nil
	case p.IsLicense():
		if p.License != nil && p.License.GroupID.Valid && p.License.GroupID.UUID == group.ID {
			return nil
		}
	case p.IsUser():
		if memberOrOwner(group) {
			return nil
		}
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// GroupWrite authorizes create/update/destroy: admin, developer and
// product bearers.
func GroupWrite(p Principal) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		if p.IsUser() {
			return nil
		}
	case p.IsProduct():
		return nil
	}

	return apperr.ForbiddenReason(ReasonAllResources)
}

// GroupOwnersRead authorizes the owners index/show like GroupShow
// (membership or ownership qualifies a user bearer).
func GroupOwnersRead(p Principal, group domain.Group, memberOrOwner func(domain.Group) bool) error {
	return GroupShow(p, group, memberOrOwner)
}

// GroupOwnersWrite authorizes attach/detach: admin, developer, product.
func GroupOwnersWrite(p Principal) error { return GroupWrite(p) }
