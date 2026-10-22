package authn_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// fakeStore answers the authn port from maps.
type fakeStore struct {
	tokens map[string]domain.Token // digest → row
	users  map[uuid.UUID]domain.User
}

func (f fakeStore) TokenByDigest(
	_ context.Context, _ uuid.UUID, digest string,
) (domain.Token, error) {
	if t, ok := f.tokens[digest]; ok {
		return t, nil
	}

	return domain.Token{}, authn.ErrNotFound
}

func (f fakeStore) UserByID(_ context.Context, _, id uuid.UUID) (domain.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}

	return domain.User{}, authn.ErrNotFound
}

func (f fakeStore) UserByEmail(
	_ context.Context, _ uuid.UUID, email string,
) (domain.User, error) {
	for id := range f.users {
		if u := f.users[id]; u.Email == email {
			return u, nil
		}
	}

	return domain.User{}, authn.ErrNotFound
}

func (f fakeStore) LicenseByKey(
	context.Context, uuid.UUID, string,
) (domain.LicenseAuth, error) {
	return domain.LicenseAuth{}, authn.ErrNotFound
}

func (f fakeStore) LicenseByID(
	context.Context, uuid.UUID, uuid.UUID,
) (domain.LicenseAuth, error) {
	return domain.LicenseAuth{}, authn.ErrNotFound
}

func (fakeStore) EnabledSecondFactorSecret(context.Context, uuid.UUID) (string, bool, error) {
	return "", false, nil
}

// basicAuth base64-encodes a Basic payload.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// TestParseCredentials covers every raw form: the four schemes, the
// Basic license:/token: prefixes and the query fallbacks.
func TestParseCredentials(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		authorization string
		query         string
		kind          authn.CredentialKind
		value         string
		secret        string
	}{
		{"bearer", "Bearer tok-1", "", authn.CredentialToken, "tok-1", ""},
		{"bearer case", "BEARER tok-2", "", authn.CredentialToken, "tok-2", ""},
		{"token scheme", "Token tok-3", "", authn.CredentialToken, "tok-3", ""},
		{"license scheme", "License lic-1", "", authn.CredentialLicense, "lic-1", ""},
		{"basic email", "Basic " + basicAuth("a@b.co", "pw"), "", authn.CredentialBasic, "a@b.co", "pw"},
		{
			"basic license prefix", "Basic " + basicAuth("license:", "lic-2"), "",
			authn.CredentialBasic, "license", "",
		},
		{"query token", "", "tok-4", authn.CredentialToken, "tok-4", ""},
		{"query license", "", "license:lic-3", authn.CredentialLicense, "lic-3", ""},
		{"query token prefix", "", "token:tok-5", authn.CredentialToken, "tok-5", ""},
		{"unknown scheme", "Digest xyz", "", authn.CredentialNone, "", ""},
		{"garbage basic", "Basic %%%%", "", authn.CredentialNone, "", ""},
	} {
		cred := authn.Parse(tc.authorization, tc.query)
		if cred.Kind != tc.kind {
			t.Errorf("%s: kind %d want %d", tc.name, cred.Kind, tc.kind)
		}

		if tc.value != "" && cred.Value != tc.value {
			t.Errorf("%s: value %q want %q", tc.name, cred.Value, tc.value)
		}

		if tc.kind == authn.CredentialLicense {
			r := cred.Resolve(authn.ModeToken)
			if r.Flow != authn.FlowLicense || r.LicenseKey == "" {
				t.Errorf("%s: license flow %+v", tc.name, r)
			}
		}

		if tc.kind == authn.CredentialToken {
			r := cred.Resolve(authn.ModeToken)
			if r.Flow != authn.FlowToken || r.TokenValue != tc.value {
				t.Errorf("%s: token flow %+v", tc.name, r)
			}
		}
	}
}

// TestResolveBasicModes checks the mode rule: email:password is a
// password flow only on POST /tokens, the legacy Basic token form
// everywhere else.
func TestResolveBasicModes(t *testing.T) {
	t.Parallel()

	cred := authn.Parse("Basic "+basicAuth("admin@example.com", "pw"), "")

	r := cred.Resolve(authn.ModePasswordOrToken)
	if r.Flow != authn.FlowPassword || r.Email != "admin@example.com" || r.Password != "pw" {
		t.Fatalf("password mode: %+v", r)
	}

	if r.ChallengeScheme() != "Basic" {
		t.Errorf("challenge: %q want Basic", r.ChallengeScheme())
	}

	r = cred.Resolve(authn.ModeToken)
	if r.Flow != authn.FlowToken || r.TokenValue != "admin@example.com" {
		t.Fatalf("token mode: %+v", r)
	}

	prefixed := authn.Parse("Basic "+basicAuth("token", "tok-6"), "")
	if pr := prefixed.Resolve(authn.ModePasswordOrToken); pr.Flow != authn.FlowToken || pr.TokenValue != "tok-6" {
		t.Errorf("token prefix in password mode: %+v", r)
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

// TestDemotedUserLosesAccess pins the security property: the token row's
// BearerRole snapshot is admin, but the LIVE user row says plain user —
// the principal must come out with the user's role and its permission
// set, not the stale admin set.
func TestDemotedUserLosesAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	account := testAccount(t)

	raw, err := crypto.GenerateTokenSecret(rand.Reader, "admin")
	if err != nil {
		t.Fatalf("secret: %v", err)
	}

	user := domain.User{
		ID:        uuid.Must(uuid.NewV7()),
		AccountID: account.ID,
		Email:     "demoted@example.com",
		Role:      domain.RoleUser, // live record: demoted
	}

	token := domain.Token{
		ID:          uuid.Must(uuid.NewV7()),
		AccountID:   account.ID,
		Digest:      crypto.DigestV3(account.Keys.SecretKey, raw),
		BearerID:    user.ID,
		BearerType:  "User",
		BearerRole:  domain.RoleAdmin, // stale snapshot
		Permissions: []string{"*"},
	}

	store := fakeStore{
		tokens: map[string]domain.Token{token.Digest: token},
		users:  map[uuid.UUID]domain.User{user.ID: user},
	}

	scenario := authn.New(store, time.Now)

	// The admin snapshot answers true; the principal must not.
	if _, ok := access.RolePermissions(domain.RoleAdmin)["admin.read"]; !ok {
		t.Fatal("sanity: admin set lacks admin.read")
	}

	principal, result, err := scenario.Authenticate(
		ctx, account, authn.Parse("Bearer "+raw, ""), authn.ModeToken, "",
	)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if principal.Role != domain.RoleUser {
		t.Errorf("role: %q want user (live record governs)", principal.Role)
	}

	if principal.Allows("admin.read") {
		t.Error("demoted principal still holds an admin permission")
	}

	if !principal.IsUser() || principal.BearerID != user.ID || principal.TokenID != token.ID {
		t.Errorf("principal: %+v", principal)
	}

	if result.Token == nil || result.Token.BearerRole != domain.RoleAdmin {
		t.Errorf("result token: %+v", result.Token)
	}

	// And the same token must authorize like a user: token.generate is
	// in the user set, admin.read is not.
	ctxWith := access.With(ctx, principal)
	if err := access.Require(ctxWith, "token.generate"); err != nil {
		t.Errorf("token.generate: %v", err)
	}

	if err := access.Require(ctxWith, "admin.read"); err == nil {
		t.Error("admin.read granted to a demoted user")
	}
}

// TestTokenShapeErrors covers the two pre-lookup token failures.
func TestTokenShapeErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	account := testAccount(t)
	scenario := authn.New(fakeStore{tokens: map[string]domain.Token{}, users: map[uuid.UUID]domain.User{}}, time.Now)

	for _, tc := range []struct {
		header string
		code   string
	}{
		{"Bearer " + uuid.Must(uuid.NewV7()).String(), "TOKEN_FORMAT_INVALID"},
		{"Bearer admin-nonexistent", "TOKEN_INVALID"},
	} {
		_, _, err := scenario.Authenticate(
			ctx, account, authn.Parse(tc.header, ""), authn.ModeToken, "",
		)
		if err == nil {
			t.Errorf("%q: expected an error", tc.header)

			continue
		}

		e := apperr.From(err)
		if e.Code != tc.code || e.Status != 401 {
			t.Errorf("%q: %v (code %q status %d)", tc.header, err, e.Code, e.Status)
		}
	}
}
