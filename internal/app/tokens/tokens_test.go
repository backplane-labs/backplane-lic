package tokens_test

import (
	"context"
	"crypto/rand"
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

func (f *fakeStore) Do(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
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

// TestGenerateUserBearerRetired pins the retired end-user
// authentication: no token may carry a user bearer — minting one's own
// or overriding to a user answers the 501, whatever the caller's role.
func TestGenerateUserBearerRetired(t *testing.T) {
	t.Parallel()

	plain := targetUser(domain.RoleUser)

	store := &fakeStore{users: map[uuid.UUID]domain.User{plain.ID: plain}}
	scenario := tokens.New(store, events.None{}, rand.Reader, time.Now)
	account := testAccount(t)

	for _, role := range []domain.Role{
		domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent,
		domain.RoleSupportRole, domain.RoleReadOnly, domain.RoleUser,
	} {
		principal := userPrincipal(role)
		ctx := access.With(context.Background(), principal)

		// Minting for oneself.
		_, _, err := scenario.Generate(ctx, account, principal, tokens.GenerateInput{})
		if e := apperr.From(err); err == nil || e.Status != 501 {
			t.Errorf("%s self: want 501, got err=%v status=%d", role, err, e.Status)
		}

		// Minting for another user.
		in := tokens.GenerateInput{BearerType: access.BearerUser, BearerID: plain.ID}

		_, _, err = scenario.Generate(ctx, account, principal, in)
		if e := apperr.From(err); err == nil || e.Status != 501 {
			t.Errorf("%s override: want 501, got err=%v status=%d", role, err, e.Status)
		}
	}

	// A product override stays the plain policy deny.
	admin := userPrincipal(domain.RoleAdmin)
	in := tokens.GenerateInput{BearerType: access.BearerProduct, BearerID: uuid.Must(uuid.NewV7())}

	_, _, err := scenario.Generate(access.With(context.Background(), admin), account, admin, in)
	if e := apperr.From(err); err == nil || e.Status != 403 {
		t.Errorf("product override: want 403, got err=%v status=%d", err, e.Status)
	}

	// A product or license bearer cannot mint even for itself.
	for _, bearer := range []string{access.BearerProduct, access.BearerLicense} {
		p := access.Principal{
			BearerType:  bearer,
			BearerID:    uuid.Must(uuid.NewV7()),
			Role:        domain.RoleLicense,
			Permissions: access.RolePermissions(domain.RoleLicense),
		}

		_, _, err := scenario.Generate(access.With(context.Background(), p), account, p, tokens.GenerateInput{})
		if e := apperr.From(err); err == nil || e.Status != 403 {
			t.Errorf("%s self: want 403, got err=%v status=%d", bearer, err, e.Status)
		}
	}
}
