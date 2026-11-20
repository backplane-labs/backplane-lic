package service_test

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"
	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/catalog"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
)

const (
	catalogProduct = "7d4c0a52-1f3e-4b6a-9c2d-5e8f1a2b3c4d"
	catalogPilot   = "0f6b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"
	catalogPaid    = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	licensingValue = "prod-1111111111111111111111111111111111111111111111111111111111111111v3"
	relayValue     = "prod-2222222222222222222222222222222222222222222222222222222222222222v3"
)

// catalogEnv is the catalog as a deployment writes it: one JSON value in
// LIC_CATALOG_PRODUCTS.
func catalogEnv(relay, pilotDuration string) map[string]string {
	return map[string]string{
		"STORE_DSN":  "postgres://lic:lic@localhost:5432/lic",
		"ACCOUNT_ID": "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f", "ACCOUNT_SLUG": "acme", "ACCOUNT_NAME": "Acme",
		"CATALOG_PRODUCTS": `[{
			"id": "` + catalogProduct + `", "name": "Komeet", "code": "komeet",
			"policies": [
				{"id": "` + catalogPilot + `", "attributes": {"name": "pilot", "duration": ` + pilotDuration + `,
					"maxMachines": 1, "machineUniquenessStrategy": "UNIQUE_PER_POLICY",
					"expirationStrategy": "REVOKE_ACCESS"}},
				{"id": "` + catalogPaid + `", "attributes": {"name": "paid", "maxMachines": 1,
					"machineUniquenessStrategy": "UNIQUE_PER_POLICY", "requireHeartbeat": true,
					"heartbeatDuration": 3600}}
			],
			"tokens": [
				{"name": "licensing", "value": "` + licensingValue + `",
					"permissions": ["license.create", "license.read", "license.update", "license.suspend",
						"license.reinstate", "license.revoke", "machine.read", "machine.delete"]},
				{"name": "relay", "value": "` + relay + `", "permissions": ["machine.read"]}
			]
		}]`,
	}
}

func TestCatalogFromConfig(t *testing.T) {
	t.Parallel()

	cfg := backplanetest.Config[service.Config](t, catalogEnv(relayValue, "2592000"))

	spec, err := service.CatalogSpec(cfg.Catalog)
	if err != nil {
		t.Fatal(err)
	}

	product := spec.Products[0]
	if product.Input.ID.String() != catalogProduct || len(product.Policies) != 2 || len(product.Tokens) != 2 ||
		product.Tokens[1].Value != relayValue || product.Policies[0].ID.String() != catalogPilot {
		t.Fatalf("spec = %+v", spec)
	}

	if strings.Contains(cfg.Catalog.Products[0].Tokens[0].Value.String(), "1111") {
		t.Fatal("the token value is not masked")
	}

	for _, tc := range []struct {
		name string
		edit func(*service.Catalog)
		want string
	}{
		{"product id", func(c *service.Catalog) { c.Products[0].ID = "nope" }, "products[0].id"},
		{"policy id", func(c *service.Catalog) { c.Products[0].Policies[0].ID = "nope" }, "policies[0].id"},
		{
			"policy attribute",
			func(c *service.Catalog) { c.Products[0].Policies[0].Attributes["maxMachines"] = "one" },
			"policies[0].attributes",
		},
		{
			"unknown policy attribute",
			func(c *service.Catalog) { c.Products[0].Policies[1].Attributes["shade"] = "red" },
			"policies[1].attributes",
		},
		{"token value", func(c *service.Catalog) { c.Products[0].Tokens[0].Value = "secret" }, "prod-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			broken := backplanetest.Config[service.Config](t, catalogEnv(relayValue, "2592000")).Catalog
			tc.edit(&broken)

			if _, err := service.CatalogSpec(broken); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CatalogSpec = %v, want an error naming %q", err, tc.want)
			}
		})
	}
}

func catalogStore(t *testing.T) (*postgres.Store, domain.Account) {
	t.Helper()

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)),
		sdkpg.Config{DSN: config.Secret(testkit.Database(t.Context(), t))})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	id := uuid.New()

	account, err := accounts.Bootstrap(t.Context(), store.Accounts,
		accounts.Spec{ID: id, Name: "Catalog", Slug: "catalog-" + id.String()})
	if err != nil {
		t.Fatal(err)
	}

	return store, account
}

func declared(t *testing.T, relay, pilotDuration string) catalog.Spec {
	t.Helper()

	spec, err := service.CatalogSpec(backplanetest.Config[service.Config](t, catalogEnv(relay, pilotDuration)).Catalog)
	if err != nil {
		t.Fatal(err)
	}

	return spec
}

// Replicas starting together reconcile one catalog: the lock lets one
// write and the others find it in place; a repeat writes nothing.
func TestCatalogReconcile(t *testing.T) {
	t.Parallel()

	store, account := catalogStore(t)
	reconciler := service.NewCatalog(store, events.None{}, service.HTTP{})
	spec := declared(t, relayValue, "2592000")

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		written int
	)

	for range 4 {
		wg.Go(func() {
			report, err := reconciler.Reconcile(t.Context(), account, spec)
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			written += report.Written
			mu.Unlock()
		})
	}

	wg.Wait()

	// One product, two policies, two tokens.
	if written != 5 {
		t.Fatalf("written across replicas = %d, want 5", written)
	}

	pilot, err := store.PolicyByID(t.Context(), account.ID, uuid.MustParse(catalogPilot))
	if err != nil || pilot.Name != "pilot" || pilot.MaxMachines == nil || *pilot.MaxMachines != 1 ||
		pilot.Duration == nil || *pilot.Duration != 2592000 || pilot.ProductID.String() != catalogProduct {
		t.Fatalf("pilot = %+v, %v", pilot, err)
	}

	relay, err := store.TokenByDigest(t.Context(), account.ID, crypto.DigestV3(account.Keys.SecretKey, relayValue))
	if err != nil || relay.BearerID.String() != catalogProduct || len(relay.Permissions) != 1 ||
		relay.Permissions[0] != "machine.read" {
		t.Fatalf("relay = %+v, %v", relay, err)
	}

	report, err := reconciler.Reconcile(t.Context(), account, spec)
	if err != nil || report.Written != 0 || len(report.Undeclared) != 0 {
		t.Fatalf("repeat = %+v, %v", report, err)
	}

	// A changed duration updates the policy; a new relay value rotates the
	// same token, and the old value no longer authenticates.
	rotated := "prod-3333333333333333333333333333333333333333333333333333333333333333v3"

	report, err = reconciler.Reconcile(t.Context(), account, declared(t, rotated, "1209600"))
	if err != nil || report.Written != 2 || !report.TokensMoved {
		t.Fatalf("change = %+v, %v", report, err)
	}

	if _, err := store.TokenByDigest(t.Context(), account.ID,
		crypto.DigestV3(account.Keys.SecretKey, relayValue)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("old relay value = %v, want not found", err)
	}

	moved, err := store.TokenByDigest(t.Context(), account.ID, crypto.DigestV3(account.Keys.SecretKey, rotated))
	if err != nil || moved.ID != relay.ID {
		t.Fatalf("rotated relay = %+v, %v", moved, err)
	}

	pilot, err = store.PolicyByID(t.Context(), account.ID, uuid.MustParse(catalogPilot))
	if err != nil || *pilot.Duration != 1209600 {
		t.Fatalf("pilot after change = %+v, %v", pilot, err)
	}
}

// A declared policy cannot change what is fixed at creation.
func TestCatalogImmutablePolicy(t *testing.T) {
	t.Parallel()

	store, account := catalogStore(t)
	reconciler := service.NewCatalog(store, events.None{}, service.HTTP{})
	spec := declared(t, relayValue, "2592000")

	if _, err := reconciler.Reconcile(t.Context(), account, spec); err != nil {
		t.Fatal(err)
	}

	encrypted := true
	spec.Products[0].Policies[1].Encrypted = &encrypted

	if _, err := reconciler.Reconcile(t.Context(), account, spec); !errors.Is(err, policies.ErrImmutable) {
		t.Fatalf("Reconcile = %v, want ErrImmutable", err)
	}
}
