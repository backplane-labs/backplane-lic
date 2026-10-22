package tokens_test

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// fakeStore records inserts and answers bearers from a map.
type fakeStore struct {
	users    map[uuid.UUID]domain.User
	inserted []domain.Token
}

func (f *fakeStore) InsertToken(_ context.Context, token domain.Token) error {
	f.inserted = append(f.inserted, token)

	return nil
}

func (f *fakeStore) TokenByID(context.Context, uuid.UUID, uuid.UUID) (domain.Token, error) {
	return domain.Token{}, domain.ErrNotFound
}

func (f *fakeStore) ListTokens(context.Context, uuid.UUID, tokens.Filter, string, uuid.UUID, paging.Params) ([]domain.Token, error) {
	return nil, nil
}

func (f *fakeStore) CountTokens(context.Context, uuid.UUID, tokens.Filter, string, uuid.UUID) (int, error) {
	return 0, nil
}

func (f *fakeStore) RegenerateToken(context.Context, uuid.UUID, uuid.UUID, string, *time.Time) error {
	return nil
}

func (f *fakeStore) DeleteToken(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeStore) UserLicensedToProduct(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeStore) LicenseOfProduct(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeStore) BearerUser(_ context.Context, _, id uuid.UUID) (domain.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}

	return domain.User{}, tokens.ErrNotFound
}

// userPrincipal builds an authenticated user of the role.
func userPrincipal(role domain.Role) access.Principal {
	return access.Principal{
		BearerType:  access.BearerUser,
		BearerID:    uuid.Must(uuid.NewV7()),
		Role:        role,
		Permissions: access.RolePermissions(role),
	}
}

func testAccount(t *testing.T) domain.Account {
	t.Helper()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}

	return domain.Account{ID: uuid.Must(uuid.NewV7()), Keys: keys}
}

func targetUser(role domain.Role) domain.User {
	return domain.User{ID: uuid.Must(uuid.NewV7()), Role: role}
}

// TestGenerateBearerOverride mirrors the reference's TokenPolicy#generate?:
// only admin and developer may mint for another bearer, and only when
// that bearer is a plain user. Everyone else's override is a 403.
func TestGenerateBearerOverride(t *testing.T) {
	t.Parallel()

	plain := targetUser(domain.RoleUser)
	adminTarget := targetUser(domain.RoleAdmin)

	store := &fakeStore{users: map[uuid.UUID]domain.User{
		plain.ID: plain, adminTarget.ID: adminTarget,
	}}

	scenario := tokens.New(store, events.None{}, rand.Reader, time.Now)
	account := testAccount(t)

	for _, tc := range []struct {
		name     string
		caller   domain.Role
		target   *domain.User
		kind     string // "" means 403
		bearerOf string
	}{
		{"admin for plain user", domain.RoleAdmin, &plain, "user", plain.ID.String()},
		{"developer for plain user", domain.RoleDeveloper, &plain, "user", plain.ID.String()},
		{"admin for admin", domain.RoleAdmin, &adminTarget, "", ""},
		{"sales agent for plain user", domain.RoleSalesAgent, &plain, "", ""},
		{"support agent for plain user", domain.RoleSupportRole, &plain, "", ""},
		{"read-only for plain user", domain.RoleReadOnly, &plain, "", ""},
		{"plain user for plain user", domain.RoleUser, &plain, "", ""},
	} {
		ctx := access.With(context.Background(), userPrincipal(tc.caller))

		in := tokens.GenerateInput{
			BearerType: access.BearerUser,
			BearerID:   tc.target.ID,
		}

		token, _, err := scenario.Generate(ctx, account, access.PrincipalOf(ctx), in)
		if tc.kind == "" {
			e := apperr.From(err)
			if err == nil || e.Status != 403 {
				t.Errorf("%s: want 403, got err=%v status=%d", tc.name, err, e.Status)
			}

			continue
		}

		if err != nil {
			t.Errorf("%s: %v", tc.name, err)

			continue
		}

		if got := string(token.BearerRole); got != tc.kind {
			t.Errorf("%s: bearer role %q want %q", tc.name, got, tc.kind)
		}

		if token.BearerID != tc.target.ID {
			t.Errorf("%s: bearer id %s want %s", tc.name, token.BearerID, tc.target.ID)
		}
	}
}

// TestGenerateSelf covers the self clause: every user role may mint its
// own token; product and license bearers may not mint at all.
func TestGenerateSelf(t *testing.T) {
	t.Parallel()

	for _, role := range []domain.Role{
		domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent,
		domain.RoleSupportRole, domain.RoleReadOnly, domain.RoleUser,
	} {
		store := &fakeStore{}
		scenario := tokens.New(store, events.None{}, rand.Reader, time.Now)
		ctx := access.With(context.Background(), userPrincipal(role))

		token, raw, err := scenario.Generate(ctx, testAccount(t), access.PrincipalOf(ctx), tokens.GenerateInput{})
		if err != nil {
			t.Errorf("%s: %v", role, err)

			continue
		}

		if prefix := crypto.TokenPrefix(string(role)); !strings.HasPrefix(raw, prefix+"-") {
			t.Errorf("%s: secret %q lacks prefix %q", role, raw, prefix)
		}

		if token.Kind != crypto.TokenKind(string(role)) {
			t.Errorf("%s: kind %q", role, token.Kind)
		}
	}

	// A product or license bearer cannot mint even for itself.
	for _, bearer := range []string{access.BearerProduct, access.BearerLicense} {
		store := &fakeStore{}
		scenario := tokens.New(store, events.None{}, rand.Reader, time.Now)

		p := access.Principal{
			BearerType:  bearer,
			BearerID:    uuid.Must(uuid.NewV7()),
			Role:        domain.RoleLicense,
			Permissions: access.RolePermissions(domain.RoleLicense),
		}

		_, _, err := scenario.Generate(access.With(context.Background(), p), testAccount(t), p, tokens.GenerateInput{})
		if e := apperr.From(err); err == nil || e.Status != 403 {
			t.Errorf("%s self: want 403, got err=%v status=%d", bearer, err, e.Status)
		}
	}
}

// TestGenerateExpiry pins the defaults: user tokens expire in 14 days,
// every other kind never expires; an explicit expiry always wins.
func TestGenerateExpiry(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	scenario := tokens.New(store, events.None{}, rand.Reader, time.Now)
	account := testAccount(t)

	user := userPrincipal(domain.RoleUser)

	token, _, err := scenario.Generate(
		access.With(context.Background(), user), account, user, tokens.GenerateInput{},
	)
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	if token.Expiry == nil {
		t.Fatal("user token: no expiry")
	}

	if got := token.Expiry.Sub(token.CreatedAt); got != tokens.UserDuration {
		t.Errorf("user token: expiry %v want %v", got, tokens.UserDuration)
	}

	admin := userPrincipal(domain.RoleAdmin)

	token, _, err = scenario.Generate(
		access.With(context.Background(), admin), account, admin, tokens.GenerateInput{},
	)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}

	if token.Expiry != nil {
		t.Errorf("admin token: expiry %v want none", token.Expiry)
	}

	explicit := time.Now().Add(24 * time.Hour)

	token, _, err = scenario.Generate(
		access.With(context.Background(), admin), account, admin,
		tokens.GenerateInput{Expiry: &explicit},
	)
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}

	if token.Expiry == nil || !token.Expiry.Equal(explicit) {
		t.Errorf("explicit expiry: %v want %v", token.Expiry, explicit)
	}
}

func (f *fakeStore) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
