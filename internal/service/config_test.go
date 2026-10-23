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
