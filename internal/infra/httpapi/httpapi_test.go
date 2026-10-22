package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// TestGetAccount smoke-tests the whole read path: migrations, account
// bootstrap, the ogen handler and the JSON:API rendering. Requires the
// compose PostgreSQL (make up); skipped without LIC_TEST_DATABASE_URL.
func TestGetAccount(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set (make up, then make test-live)")
	}

	ctx := context.Background()

	store, err := postgres.Open(ctx, xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer store.Close()

	id := uuid.New()
	spec := accounts.Spec{ID: id, Name: "Test Account", Slug: "test-" + id.String()[:8]}

	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	handler := httpapi.New(store.Accounts, httpapi.AccountSpec{ID: spec.ID, Slug: spec.Slug})

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/"+spec.ID.String(), http.NoBody)
	req.Header.Set("Accept", "application/vnd.api+json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/vnd.api+json") {
		t.Errorf("content type: %q", ct)
	}

	// The wire format is camelCase, hence tagliatelle exemptions below.
	var doc struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Name    string `json:"name"`
				Slug    string `json:"slug"`
				Created string `json:"created"`
				Updated string `json:"updated"`
			} `json:"attributes"`
			Relationships map[string]struct {
				Links struct {
					Related string `json:"related"`
				} `json:"links"`
			} `json:"relationships"`
			Links struct {
				Self string `json:"self"`
			} `json:"links"`
		} `json:"data"`
		Meta struct {
			PublicKey string `json:"publicKey"` //nolint:tagliatelle // wire format
			Keys      struct {
				Ed25519 string `json:"ed25519"`
				Rsa2048 string `json:"rsa2048"`
				Ecdsa   string `json:"ecdsa"`
			} `json:"keys"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}

	if doc.Data.Type != "accounts" || doc.Data.ID != spec.ID.String() {
		t.Errorf("resource: %s %s", doc.Data.Type, doc.Data.ID)
	}

	if doc.Data.Attributes.Name != spec.Name || doc.Data.Attributes.Slug != spec.Slug {
		t.Errorf("attributes: %+v", doc.Data.Attributes)
	}

	// RFC3339 with exactly 3 fractional digits and a Z suffix.
	for _, ts := range []string{doc.Data.Attributes.Created, doc.Data.Attributes.Updated} {
		if !strings.HasSuffix(ts, "Z") || ts[len(ts)-5] != '.' {
			t.Errorf("timestamp: %q", ts)
		}
	}

	base := "/v1/accounts/" + spec.ID.String()
	if doc.Data.Links.Self != base {
		t.Errorf("self link: %q", doc.Data.Links.Self)
	}

	if got := doc.Data.Relationships["licenses"].Links.Related; got != base+"/licenses" {
		t.Errorf("licenses link: %q", got)
	}

	if len(doc.Data.Relationships) != 17 {
		t.Errorf("relationships: %d, want 17", len(doc.Data.Relationships))
	}

	if doc.Meta.PublicKey == "" || doc.Meta.Keys.Ed25519 == "" ||
		doc.Meta.Keys.Rsa2048 == "" || doc.Meta.Keys.Ecdsa == "" {
		t.Errorf("meta keys: %+v", doc.Meta)
	}
}
