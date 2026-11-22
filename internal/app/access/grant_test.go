package access_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func tokenPrincipal(role domain.Role, permissions ...string) access.Principal {
	return access.Principal{
		BearerType: access.BearerProduct, BearerID: uuid.New(), Role: role, TokenID: uuid.New(),
		Permissions: access.Effective(role, permissions),
	}
}

func TestGrantablePermissions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		p    access.Principal
		want []string
	}{
		{"wildcard token", tokenPrincipal(domain.RoleProduct, "*"), []string{"*"}},
		{"password session", access.Principal{Role: domain.RoleAdmin, Permissions: access.RolePermissions(domain.RoleAdmin)}, []string{"*"}},
		{"narrowed token", tokenPrincipal(domain.RoleProduct, "token.generate", "machine.read"), []string{"machine.read", "token.generate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := access.GrantablePermissions(tc.p); !slices.Equal(got, tc.want) {
				t.Fatalf("GrantablePermissions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCovers(t *testing.T) {
	t.Parallel()

	narrowed := tokenPrincipal(domain.RoleProduct, "token.regenerate", "machine.read")

	for _, tc := range []struct {
		name   string
		p      access.Principal
		role   domain.Role
		target []string
		want   bool
	}{
		{"wide caller, wide target", tokenPrincipal(domain.RoleProduct, "*"), domain.RoleProduct, []string{"*"}, true},
		{"narrowed caller, wildcard product target", narrowed, domain.RoleProduct, []string{"*"}, false},
		{"narrowed caller, narrower target", narrowed, domain.RoleProduct, []string{"machine.read"}, true},
		{"narrowed caller, other permission", narrowed, domain.RoleProduct, []string{"license.create"}, false},
		{"narrowed caller, own set", narrowed, domain.RoleProduct, []string{"token.regenerate", "machine.read"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := access.Covers(tc.p, tc.role, tc.target); got != tc.want {
				t.Fatalf("Covers = %v, want %v", got, tc.want)
			}
		})
	}
}

func BenchmarkCovers(b *testing.B) {
	p := tokenPrincipal(domain.RoleProduct, "token.regenerate", "machine.read")
	for b.Loop() {
		access.Covers(p, domain.RoleProduct, []string{"*"})
	}
}
