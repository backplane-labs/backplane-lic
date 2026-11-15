package service_test

import (
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"

	"github.com/backplane-labs/backplane-lic/internal/service"
)

// An installation configures only what it needs: the schema must accept a
// configuration without the optional blocks (brand, portal, automation).
func TestConfigLoadsWithoutOptionalBlocks(t *testing.T) {
	t.Parallel()

	cfg := backplanetest.Config[service.Config](t, map[string]string{
		"STORE_DSN":  "postgres://lic:lic@localhost:5432/lic",
		"ACCOUNT_ID": "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f", "ACCOUNT_SLUG": "acme", "ACCOUNT_NAME": "Acme",
	})
	if cfg.Cache != nil {
		t.Fatalf("cache = %+v, want absent", cfg.Cache)
	}

	if cfg.Brand.Name != "" {
		t.Fatalf("brand name = %q, want unset (the default applies at startup)", cfg.Brand.Name)
	}

	branded := backplanetest.Config[service.Config](t, map[string]string{
		"STORE_DSN":          "postgres://lic:lic@localhost:5432/lic",
		"ACCOUNT_ID":         "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f",
		"ACCOUNT_SLUG":       "acme",
		"ACCOUNT_NAME":       "Acme",
		"BRAND_NAME":         "Acme",
		"BRAND_ACCENT_COLOR": "#3366ff",
		"CACHE_ADDR":         "127.0.0.1:6390",
	})
	if branded.Cache == nil || branded.Cache.Addr != "127.0.0.1:6390" {
		t.Fatalf("cache = %+v, want the configured address", branded.Cache)
	}

	if branded.Brand.Name != "Acme" || branded.Brand.AccentColor != "#3366ff" {
		t.Fatalf("brand = %+v", branded.Brand)
	}
}

// rfc8032Seed is RFC 8032's first ed25519 test vector.
const rfc8032Seed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

// The vendor's signing seed comes from LIC_ACCOUNT_ED25519_SEED; a malformed
// one stops startup.
func TestAccountSeedFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, seed string
		wantErr    bool
		wantSeeded bool
	}{
		{name: "generated", seed: ""},
		{name: "imported", seed: rfc8032Seed, wantSeeded: true},
		{name: "not hex", seed: "zz", wantErr: true},
		{name: "short", seed: rfc8032Seed[:62], wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := backplanetest.Config[service.Config](t, map[string]string{
				"STORE_DSN":  "postgres://lic:lic@localhost:5432/lic",
				"ACCOUNT_ID": "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f", "ACCOUNT_SLUG": "acme", "ACCOUNT_NAME": "Acme",
				"ACCOUNT_ED25519_SEED": tc.seed,
			})

			spec, err := service.AccountSpec(cfg.Account)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}

			if !tc.wantErr && spec.Ed25519.IsZero() == tc.wantSeeded {
				t.Fatalf("seeded = %v, want %v", !spec.Ed25519.IsZero(), tc.wantSeeded)
			}
		})
	}
}
