package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
)

// basic base64-encodes a Basic credential.
// pipeStore answers the authn port with one admin user and her token
// (digest-addressed).
type pipeStore struct {
	account domain.Account
	token   domain.Token
	user    domain.User
}

func (s pipeStore) TokenByDigest(
	_ context.Context, _ uuid.UUID, digest string,
) (domain.Token, error) {
	if digest == s.token.Digest {
		return s.token, nil
	}

	return domain.Token{}, authn.ErrNotFound
}

func (s pipeStore) UserByID(_ context.Context, _, id uuid.UUID) (domain.User, error) {
	if id == s.user.ID {
		return s.user, nil
	}

	return domain.User{}, authn.ErrNotFound
}

func (s pipeStore) UserByEmail(
	_ context.Context, _ uuid.UUID, email string,
) (domain.User, error) {
	if email == s.user.Email {
		return s.user, nil
	}

	return domain.User{}, authn.ErrNotFound
}

func (pipeStore) LicenseByKey(context.Context, uuid.UUID, string) (domain.LicenseAuth, error) {
	return domain.LicenseAuth{}, authn.ErrNotFound
}

func (pipeStore) LicenseByID(context.Context, uuid.UUID, uuid.UUID) (domain.LicenseAuth, error) {
	return domain.LicenseAuth{}, authn.ErrNotFound
}

func (pipeStore) EnabledSecondFactorSecret(context.Context, uuid.UUID) (string, bool, error) {
	return "", false, nil
}

type fakeAccounts struct {
	account domain.Account
}

func (f fakeAccounts) Account(context.Context, uuid.UUID) (domain.Account, error) {
	return f.account, nil
}

func pipeSetup(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}

	account := domain.Account{ID: uuid.Must(uuid.NewV7()), Slug: "example-com", Keys: keys}
	user := domain.User{ID: uuid.Must(uuid.NewV7()), Role: domain.RoleAdmin}

	raw, err := crypto.GenerateTokenSecret(rand.Reader, "admin")
	if err != nil {
		t.Fatalf("secret: %v", err)
	}

	token := domain.Token{
		ID:          uuid.Must(uuid.NewV7()),
		Digest:      crypto.DigestV3(keys.SecretKey, raw),
		BearerID:    user.ID,
		BearerType:  "User",
		Permissions: []string{"*"},
	}

	store := pipeStore{account: account, token: token, user: user}
	clock := time.Now

	handler := httpapi.New(httpapi.Deps{
		Accounts: fakeAccounts{account},
		Authn:    authn.New(store, clock),
		Users:    users.New(userStore{store: store}, events.None{}, clock),
		Tokens:   tokens.New(tokenStore{}, events.None{}, rand.Reader, clock),
		Spec:     httpapi.Spec{ID: account.ID, Slug: account.Slug},
		Revision: "deadbeef",
		Clock:    clock,
	})

	return handler, account.ID.String(), raw
}

type userStore struct {
	users.Store

	store pipeStore
}

func (u userStore) UserByID(ctx context.Context, a, id uuid.UUID) (domain.User, error) {
	return u.store.UserByID(ctx, a, id)
}

func (u userStore) UserByEmail(ctx context.Context, a uuid.UUID, e string) (domain.User, error) {
	return u.store.UserByEmail(ctx, a, e)
}

func (userStore) InsertUser(context.Context, domain.User) error { return nil }

func (userStore) UserHasActiveLicense(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

type tokenStore struct {
	tokens.Store
}

func (tokenStore) InsertToken(context.Context, domain.Token) error { return nil }

func (tokenStore) BearerUser(context.Context, uuid.UUID, uuid.UUID) (domain.User, error) {
	return domain.User{}, tokens.ErrNotFound
}

// TestPipelinePlain covers the two unsigned routes and the default 404.
func TestPipelinePlain(t *testing.T) {
	t.Parallel()

	handler, _, _ := pipeSetup(t)

	for _, tc := range []struct {
		target string
		status int
	}{
		{"/v1/ping", http.StatusOK},
		{"/v1/health", http.StatusNoContent},
		{"/nope", http.StatusNotFound},
		{"/v1/me/me", http.StatusNotFound},
		{"/ping", http.StatusNotFound}, // outside /v1: 404, no bare rewrite
	} {
		rec := do(t, handler, http.MethodGet, tc.target, "", http.NoBody)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d want %d: %s", tc.target, rec.Code, tc.status, rec.Body)
		}

		if rec.Header().Get("Date") != "" {
			t.Errorf("%s: unsigned route must send no Date", tc.target)
		}
	}

	rec := do(t, handler, http.MethodGet, "/v1/ping", "", http.NoBody)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("ping content type: %q", ct)
	}

	if rec.Body.String() != " " {
		t.Errorf("ping body: %q", rec.Body.String())
	}
}

// TestPipelineMiddlewareErrors covers the pre-handler failures: content
// negotiation, the API version check and body decoding (none of them
// carry a meta object or the Keygen headers).
func TestPipelineMiddlewareErrors(t *testing.T) {
	t.Parallel()

	handler, id, _ := pipeSetup(t)

	cases := []struct {
		name   string
		method string
		target string
		accept string
		status int
		code   string
	}{
		{
			name: "mime", method: http.MethodGet, target: "/v1/accounts/" + id,
			accept: "text/html", status: 400, code: "MIME_TYPE_INVALID",
		},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.target, http.NoBody)
		req.Header.Set("Accept", tc.accept)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != tc.status {
			t.Errorf("%s: status %d: %s", tc.name, rec.Code, rec.Body)
		}

		if code := errorCode(t, rec.Body.Bytes()); code != tc.code {
			t.Errorf("%s: code %q want %q", tc.name, code, tc.code)
		}

		if strings.Contains(rec.Body.String(), `"meta"`) {
			t.Errorf("%s: middleware errors carry no meta: %s", tc.name, rec.Body)
		}
	}

	// The version check rides its own header.
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/"+id, http.NoBody)
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Keygen-Version", "9.9")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 400 || errorCode(t, rec.Body.Bytes()) != "INVALID_API_VERSION" {
		t.Errorf("version: status %d: %s", rec.Code, rec.Body)
	}

	if rec.Header().Get("Keygen-Edition") != "" {
		t.Error("version error must carry no Keygen headers")
	}

	// Bad JSON on the token route: the JSON_INVALID middleware error.
	req = httptest.NewRequest(http.MethodPost, "/v1/accounts/"+id+"/tokens",
		strings.NewReader("{nope"))
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("a@b.co:pw")))

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 400 || errorCode(t, rec.Body.Bytes()) != "JSON_INVALID" {
		t.Errorf("json: status %d: %s", rec.Code, rec.Body)
	}
}

// TestGenerateBodyValidation covers the typed-params order over an
// authenticated request: unpermitted beats missing, and the pointers.
func TestGenerateBodyValidation(t *testing.T) {
	t.Parallel()

	handler, id, raw := pipeSetup(t)
	base := "/v1/accounts/" + id

	for _, tc := range []struct {
		name    string
		body    string
		status  int
		code    string
		pointer string
	}{
		{
			"unpermitted kind", `{"data":{"type":"tokens","attributes":{"kind":"admin-token"}}}`,
			400, "", "/data/attributes/kind",
		},
		{"missing type", `{"data":{}}`, 400, "", "/data/type"},
		{"invalid type", `{"data":{"type":"licenses"}}`, 400, "", "/data/type"},
		{"otp is unpermitted", `{"meta":{"otp":"123456"}}`, 400, "", "/meta/otp"},
		{"ok (user bearer is retired)", `{"data":{"type":"tokens"}}`, 501, "NOT_IMPLEMENTED", ""},
	} {
		rec := do(t, handler, http.MethodPost, base+"/tokens", "Bearer "+raw,
			strings.NewReader(tc.body))

		var doc struct {
			Errors []struct {
				Code   string `json:"code"`
				Source struct {
					Pointer string `json:"pointer"`
				} `json:"source"`
			} `json:"errors"`
		}

		decode(t, rec.Body.Bytes(), &doc)

		if rec.Code != tc.status {
			t.Errorf("%s: status %d: %s", tc.name, rec.Code, rec.Body)

			continue
		}

		if tc.status == 400 && tc.pointer != "" {
			e := doc.Errors[0]
			if e.Source.Pointer != tc.pointer {
				t.Errorf("%s: pointer %q want %q", tc.name, e.Source.Pointer, tc.pointer)
			}
		}
	}
}
