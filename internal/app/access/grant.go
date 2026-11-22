package access

import (
	"slices"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// narrowed reports whether p acts with less than its role's set: a token
// whose own permissions narrow the bearer's (Keygen EE).
func (p Principal) narrowed() bool {
	return p.IsTokenAuthn() && len(p.Permissions) < len(RolePermissions(p.Role))
}

// GrantablePermissions are the permissions of a token p creates: "*" when
// p acts with its whole role, else exactly p's effective set. A narrowed
// token never mints a wider one; the new token's own role intersects it
// again when it authenticates.
func GrantablePermissions(p Principal) []string {
	if !p.narrowed() {
		return []string{"*"}
	}

	out := make([]string, 0, len(p.Permissions))
	for perm := range p.Permissions {
		out = append(out, perm)
	}

	slices.Sort(out)

	return out
}

// Covers reports whether p holds every permission a token of role with
// tokenPermissions grants, so handing out its secret (a regeneration)
// gives no one more than p has.
func Covers(p Principal, role domain.Role, tokenPermissions []string) bool {
	if !p.narrowed() {
		return true
	}

	for perm := range Effective(role, tokenPermissions) {
		if _, ok := p.Permissions[perm]; !ok {
			return false
		}
	}

	return true
}
