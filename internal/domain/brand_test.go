package domain_test

import (
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestNewBrand(t *testing.T) {
	t.Parallel()

	brand, err := domain.NewBrand(domain.Brand{})
	if err != nil || brand.Name != domain.DefaultBrandName {
		t.Fatalf("empty brand = %+v, %v; want the default name", brand, err)
	}

	valid := domain.Brand{
		Name: " Acme ", LogoURL: "https://cdn.acme.test/logo.svg", FaviconURL: "https://cdn.acme.test/icon.png",
		AccentColor: "#3366ff", SupportEmail: "help@acme.test", SupportURL: "https://acme.test/help",
		TermsURL: "https://acme.test/terms", PrivacyURL: "https://acme.test/privacy",
	}

	brand, err = domain.NewBrand(valid)
	if err != nil {
		t.Fatalf("valid brand: %v", err)
	}

	if brand.Name != "Acme" {
		t.Errorf("name = %q, want trimmed", brand.Name)
	}

	if origins := brand.Origins(); len(origins) != 1 || origins[0] != "https://cdn.acme.test" {
		t.Errorf("origins = %v", origins)
	}

	for name, bad := range map[string]domain.Brand{
		"http logo":       {LogoURL: "http://acme.test/logo.svg"},
		"javascript url":  {SupportURL: "javascript:alert(1)"},
		"relative url":    {TermsURL: "/terms"},
		"quoted url":      {PrivacyURL: `https://acme.test/"onload`},
		"named color":     {AccentColor: "red"},
		"short color":     {AccentColor: "#36f"},
		"display email":   {SupportEmail: "Acme <help@acme.test>"},
		"long tagline":    {Tagline: string(make([]rune, 121))},
		"credentials url": {LogoURL: "https://user:pass@acme.test/logo.svg"},
	} {
		if _, err := domain.NewBrand(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
