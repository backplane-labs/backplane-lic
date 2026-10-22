package testkit_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
)

// TestSeedAdmin verifies the database seeding helper against the compose
// PostgreSQL: the admin user exists with the admin role after the call,
// and a second call on the same email is a no-op. Requires make up;
// skipped without LIC_TEST_DATABASE_URL.
func TestSeedAdmin(t *testing.T) {
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
	spec := accounts.Spec{ID: id, Name: "Testkit Account", Slug: "tk-" + id.String()[24:36]}

	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	raw, err := testkit.SeedAdmin(ctx, store, id, "tk-admin@example.com")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The raw secret is a v3 admin token (the <role>-<hex>v3 shape).
	if !strings.HasSuffix(raw, "v3") || !strings.HasPrefix(raw, "admin-") {
		t.Fatalf("raw token shape: %q", raw)
	}

	if _, err := testkit.SeedAdmin(ctx, store, id, "tk-admin@example.com"); err != nil {
		t.Fatalf("reseed: %v", err)
	}

	user, err := store.UserByEmail(ctx, id, "tk-admin@example.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if user.Role != domain.RoleAdmin {
		t.Errorf("role %q want %q", user.Role, domain.RoleAdmin)
	}

	if _, err := store.UserByEmail(ctx, id, "nobody@example.com"); err == nil {
		t.Error("unrelated email unexpectedly found")
	}
}
