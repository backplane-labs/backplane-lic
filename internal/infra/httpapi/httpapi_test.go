package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
)

const (
	adminEmail    = "ci-admin@example.com"
	adminPassword = "ci-password-123"
)

var meID string

// TestEndToEnd exercises the whole pipeline against the compose
// PostgreSQL: bootstrap, the seeded admin token, Bearer authentication,
// response signing and the me and profile reads, and the 501 of the
// retired end-user authentication. Requires make up; skipped without
// LIC_TEST_DATABASE_URL.
func TestEndToEnd(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set (make up first)")
	}

	ctx := context.Background()

	store, err := postgres.Open(ctx, xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer store.Close()

	id := uuid.Must(uuid.NewV7())
	spec := accounts.Spec{ID: id, Name: "CI Account", Slug: "ci-" + id.String()[24:36]}

	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	clock := time.Now

	adminToken, err := testkit.SeedAdmin(ctx, store, id, adminEmail)
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	account, err := store.Accounts.Account(ctx, id)
	if err != nil {
		t.Fatalf("account: %v", err)
	}

	handler := httpapi.New(httpapi.Deps{
		Accounts: store.Accounts,
		Authn:    authn.New(store, clock),
		Users:    users.New(store, events.None{}, clock),
		Tokens:   tokens.New(store, events.None{}, rand.Reader, clock),
		Groups:   groups.New(store, events.None{}, clock),
		Spec:     httpapi.Spec{ID: id, Slug: spec.Slug},
		Revision: "deadbeef",
		Clock:    clock,
	})

	base := "/v1/accounts/" + id.String()

	// 1. Anonymous read: the TOKEN_MISSING 401 with the Bearer
	// challenge.
	rec := do(t, handler, http.MethodGet, base, "", http.NoBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: status %d: %s", rec.Code, rec.Body.String())
	}

	if wa := rec.Header().Get("WWW-Authenticate"); wa != `Bearer realm="keygen"` {
		t.Errorf("anonymous: WWW-Authenticate %q", wa)
	}

	if code := errorCode(t, rec.Body.Bytes()); code != "TOKEN_MISSING" {
		t.Errorf("anonymous: code %q want TOKEN_MISSING", code)
	}

	// 2. The Basic email:password credential answers the 501
	// everywhere (end-user authentication is out of scope).
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(adminEmail+":"+adminPassword))
	body := `{"data":{"type":"tokens","attributes":{"name":"ci"}}}`

	rec = do(t, handler, http.MethodPost, base+"/tokens", basic, strings.NewReader(body))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("basic tokens: status %d: %s", rec.Code, rec.Body.String())
	}

	// 3. The seeded admin token authenticates; POST /tokens still
	// answers the 501 (user-bearer tokens are retired).
	bearer := "Bearer " + adminToken
	rec = do(t, handler, http.MethodPost, base+"/tokens", bearer, strings.NewReader(body))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("tokens: status %d: %s", rec.Code, rec.Body.String())
	}

	if rec.Header().Get("Keygen-Bearer") == "" {
		t.Error("Keygen-Bearer: empty for an authenticated request")
	}

	verifySignature(t, rec, account, http.MethodPost, base+"/tokens")

	raw := adminToken

	// 4. Read the account with the seeded token; verify the signature.
	rec = do(t, handler, http.MethodGet, base, "Bearer "+raw, http.NoBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("account: status %d: %s", rec.Code, rec.Body.String())
	}

	var acct struct {
		Data struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	}

	decode(t, rec.Body.Bytes(), &acct)

	if acct.Data.Type != "accounts" || acct.Data.ID != id.String() {
		t.Errorf("account document: %+v", acct.Data)
	}

	if v := rec.Header().Get("Keygen-Version"); v != "1.8" {
		t.Errorf("Keygen-Version: %q", v)
	}

	verifySignature(t, rec, account, http.MethodGet, base)

	// 4. /me: the user document with the current token in included (and
	// no secret on it).
	rec = do(t, handler, http.MethodGet, "/v1/me", "Bearer "+raw, http.NoBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d: %s", rec.Code, rec.Body.String())
	}

	var meDoc struct {
		Data struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				Email string `json:"email"`
				Role  string `json:"role"`
			} `json:"attributes"`
		} `json:"data"`
		Included []struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Token any `json:"token"`
			} `json:"attributes"`
		} `json:"included"`
	}

	decode(t, rec.Body.Bytes(), &meDoc)

	meID = meDoc.Data.ID

	if meDoc.Data.Type != "users" || meDoc.Data.Attributes.Email != adminEmail ||
		meDoc.Data.Attributes.Role != "admin" {
		t.Errorf("me document: %+v", meDoc.Data)
	}

	if len(meDoc.Included) != 1 || meDoc.Included[0].Type != "tokens" ||
		meDoc.Included[0].Attributes.Token != nil {
		t.Errorf("me included: %+v", meDoc.Included)
	}

	tokenID := meDoc.Included[0].ID

	// 5. /profile: meta.tokenId carries the current token's id.
	rec = do(t, handler, http.MethodGet, "/v1/profile", "Bearer "+raw, http.NoBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("profile: status %d: %s", rec.Code, rec.Body.String())
	}

	var profile struct {
		Meta struct {
			TokenID string `json:"tokenId"` //nolint:tagliatelle // the wire format is camelCase
		} `json:"meta"`
	}

	decode(t, rec.Body.Bytes(), &profile)

	if profile.Meta.TokenID != tokenID {
		t.Errorf("profile meta.tokenId: %q want %q", profile.Meta.TokenID, tokenID)
	}

	// 6. The retired end-user authentication routes answer the 501.
	for _, path := range []string{
		base + "/passwords",
		base + "/users/" + meID + "/actions/update-password",
		base + "/users/" + meID + "/actions/reset-password",
		base + "/users/" + meID + "/second-factors",
		base + "/users/" + meID + "/tokens",
	} {
		rec = do(t, handler, http.MethodPost, path, bearer, strings.NewReader(`{"data":{}}`))

		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: status %d want 501: %s", path, rec.Code, rec.Body.String())
		}

		if code := errorCode(t, rec.Body.Bytes()); code != "NOT_IMPLEMENTED" {
			t.Errorf("%s: code %q want NOT_IMPLEMENTED", path, code)
		}
	}
}

// do runs one request through the handler.
func do(t *testing.T, h http.Handler, method, target, authorization string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Accept", "application/vnd.api+json")

	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// decode unmarshals a JSON:API document.
func decode(t *testing.T, b []byte, v any) {
	t.Helper()

	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode: %v: %s", err, b)
	}
}

// errorCode reads the first error's code.
func errorCode(t *testing.T, b []byte) string {
	t.Helper()

	var doc struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}

	decode(t, b, &doc)

	if len(doc.Errors) == 0 {
		t.Fatalf("no errors: %s", b)
	}

	return doc.Errors[0].Code
}

var signatureRe = regexp.MustCompile(`algorithm="([^"]+)", signature="([^"]+)"`)

// verifySignature checks the response's ed25519 Keygen-Signature against
// the account's public key over the reconstructed signing string.
func verifySignature(t *testing.T, rec *httptest.ResponseRecorder, account domain.Account, method, uri string) {
	t.Helper()

	header := rec.Header().Get("Keygen-Signature")
	if header == "" {
		t.Fatal("no Keygen-Signature header")
	}

	m := signatureRe.FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("signature header: %q", header)
	}

	if m[1] != crypto.AlgEd25519 {
		t.Fatalf("algorithm: %q", m[1])
	}

	sig, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		t.Fatalf("signature b64: %v", err)
	}

	pub, err := hex.DecodeString(account.Keys.Ed25519Public)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}

	date := rec.Header().Get("Date")
	digest := rec.Header().Get("Digest")
	data := crypto.SigningData(method, uri, "example.com", date, digest)

	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(data), sig) {
		t.Errorf("signature does not verify (date %q)", date)
	}

	if rec.Header().Get("Keygen-Digest") != digest || rec.Header().Get("Keygen-Date") != date {
		t.Error("Keygen mirror headers missing")
	}
}
