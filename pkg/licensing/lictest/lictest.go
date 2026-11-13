// Package lictest runs a real backplane-lic server inside a test's process,
// for integration tests of what uses the licensing API: the server's own
// HTTP handler on a loopback port, over a PostgreSQL the caller provides
// (migrated on start), in an account of its own, so tests share one
// database and run in parallel. An admin token is seeded; Product,
// ProductToken, Policy and License build a fixture through the API.
//
//	srv := lictest.Start(t, dsn)
//	product := srv.Product(t, "app")
//	policy := srv.Policy(t, product, lictest.Attributes{"maxMachines": 1, "floating": false})
//	license := srv.License(t, policy, lictest.Attributes{"metadata": map[string]any{"plan": "pro"}})
//	vendor := srv.Client(t, licensing.Config{Token: srv.ProductToken(t, product)})
//	user := srv.Client(t, licensing.Config{LicenseKey: license.Key})
package lictest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// Server is a running licensing server with its account.
type Server struct {
	// URL is the API origin (http://127.0.0.1:port): licensing.Config's
	// APIURL. The account is also reachable as /v1/accounts/{AccountID}.
	URL string
	// AccountID and Slug name the server's account.
	AccountID, Slug string
	// AdminToken is a bearer token of the account's admin.
	AdminToken string
	// PublicKey is the account's Ed25519 verify key, hex: licensing.Config's
	// PublicKey, which verifies responses, license files and machine files.
	PublicKey string
}

// Attributes are JSON:API attributes as the API takes them (camelCase
// names, Keygen's semantics); the server validates them.
type Attributes map[string]any

// License is a created license.
type License struct {
	ID, Key string
}

// Start runs a server over the PostgreSQL at dsn in a new account; it
// stops when the test ends.
func Start(tb testing.TB, dsn string) *Server {
	tb.Helper()

	store, err := postgres.Open(tb.Context(), xlog.New(xlog.NopCore{}), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		tb.Fatalf("lictest: open the database: %v", err)
	}

	tb.Cleanup(store.Close)

	id := uuid.New()
	spec := accounts.Spec{ID: id, Slug: "lictest-" + id.String()[:8], Name: "lictest " + id.String()[:8]}

	if _, err := accounts.Bootstrap(tb.Context(), store.Accounts, spec); err != nil {
		tb.Fatalf("lictest: bootstrap the account: %v", err)
	}

	admin, err := testkit.SeedAdmin(tb.Context(), store, spec.ID, "admin@"+spec.Slug+".test")
	if err != nil {
		tb.Fatalf("lictest: seed the admin: %v", err)
	}

	handler, err := service.Handler(store, spec, service.HTTP{}, "lictest.test", "lictest")
	if err != nil {
		tb.Fatalf("lictest: %v", err)
	}

	srv := httptest.NewServer(handler)
	tb.Cleanup(srv.Close)

	s := &Server{URL: srv.URL, AccountID: id.String(), Slug: spec.Slug, AdminToken: admin}
	s.PublicKey = s.publicKey(tb)

	return s
}

// Client is a licensing client of the server: cfg with APIURL and
// PublicKey filled in.
func (s *Server) Client(tb testing.TB, cfg licensing.Config) *licensing.Client {
	tb.Helper()

	cfg.APIURL, cfg.PublicKey = s.URL, s.PublicKey

	client, err := licensing.New(cfg)
	if err != nil {
		tb.Fatalf("lictest: client: %v", err)
	}

	return client
}

// Product creates a product named name and answers its id.
func (s *Server) Product(tb testing.TB, name string) string {
	tb.Helper()

	return s.create(tb, "/products", "products", Attributes{"name": name}, nil).ID
}

// ProductToken creates a bearer token of the product (what a vendor-side
// service authenticates with).
func (s *Server) ProductToken(tb testing.TB, productID string) string {
	tb.Helper()

	return s.create(tb, "/products/"+productID+"/tokens", "tokens", nil, nil).Attributes.Token
}

// Policy creates a policy of the product: attrs over the defaults
// {"name": "lictest policy", "scheme": "ED25519_SIGN"}; answers its id.
func (s *Server) Policy(tb testing.TB, productID string, attrs Attributes) string {
	tb.Helper()

	all := Attributes{"name": "lictest policy", "scheme": "ED25519_SIGN"}
	maps.Copy(all, attrs)

	return s.create(tb, "/policies", "policies", all, relation("product", "products", productID)).ID
}

// License creates a license of the policy with attrs (name, metadata,
// expiry, ...).
func (s *Server) License(tb testing.TB, policyID string, attrs Attributes) License {
	tb.Helper()

	created := s.create(tb, "/licenses", "licenses", attrs, relation("policy", "policies", policyID))

	return License{ID: created.ID, Key: created.Attributes.Key}
}

// Do sends an admin request to path (under the account, e.g. "/machines")
// with an optional JSON:API body and answers the status and body.
func (s *Server) Do(tb testing.TB, method, path string, body any) (int, []byte) {
	tb.Helper()

	var reader io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("lictest: %v", err)
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(tb.Context(), method, s.URL+"/v1/accounts/"+s.AccountID+path, reader)
	if err != nil {
		tb.Fatalf("lictest: %v", err)
	}

	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tb.Fatalf("lictest: %s %s: %v", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		tb.Fatalf("lictest: %s %s: %v", method, path, err)
	}

	return resp.StatusCode, raw
}

// resource is the slice of a created resource the helpers read.
type resource struct {
	ID         string `json:"id"`
	Attributes struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	} `json:"attributes"`
	Meta struct {
		Keys struct {
			Ed25519 string `json:"ed25519"`
		} `json:"keys"`
	} `json:"meta"`
}

func relation(name, kind, id string) map[string]any {
	return map[string]any{name: map[string]any{"data": map[string]any{"type": kind, "id": id}}}
}

func (s *Server) create(tb testing.TB, path, kind string, attrs Attributes, relationships map[string]any) resource {
	tb.Helper()

	data := map[string]any{"type": kind}
	if attrs != nil {
		data["attributes"] = attrs
	}

	if relationships != nil {
		data["relationships"] = relationships
	}

	status, raw := s.Do(tb, http.MethodPost, path, map[string]any{"data": data})
	// Keygen answers a token with 200 and every other resource with 201.
	if status != http.StatusCreated && status != http.StatusOK {
		tb.Fatalf("lictest: POST %s = %d: %s", path, status, raw)
	}

	var doc struct {
		Data resource `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		tb.Fatalf("lictest: POST %s: %v", path, err)
	}

	return doc.Data
}

// publicKey reads the account's Ed25519 verify key (base64 of its hex).
func (s *Server) publicKey(tb testing.TB) string {
	tb.Helper()

	status, raw := s.Do(tb, http.MethodGet, "", nil)
	if status != http.StatusOK {
		tb.Fatalf("lictest: account = %d: %s", status, raw)
	}

	var doc struct {
		Data resource `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		tb.Fatalf("lictest: account: %v", err)
	}

	hex, err := base64.StdEncoding.DecodeString(doc.Data.Meta.Keys.Ed25519)
	if err != nil {
		tb.Fatalf("lictest: account key: %v", err)
	}

	return string(hex)
}
