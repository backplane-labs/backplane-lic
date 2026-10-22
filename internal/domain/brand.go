package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// DefaultBrandName names an installation that configures no brand.
const DefaultBrandName = "Licenses"

var (
	errBrandURL   = errors.New("must be an absolute https URL")
	errBrandColor = errors.New("must be a #RRGGBB color")
	errBrandEmail = errors.New("must be an email address")
	errBrandText  = errors.New("must be at most 120 characters")

	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Brand is the vendor identity the installation shows its customers: the
// portal's header, colors and footer links, and the data every message
// template receives. It is immutable after NewBrand.
type Brand struct {
	Name         string `json:"name"`
	Tagline      string `json:"tagline,omitempty"`
	LogoURL      string `json:"logoUrl,omitempty"`      //nolint:tagliatelle // frontend configuration is camelCase
	FaviconURL   string `json:"faviconUrl,omitempty"`   //nolint:tagliatelle // frontend configuration is camelCase
	AccentColor  string `json:"accentColor,omitempty"`  //nolint:tagliatelle // frontend configuration is camelCase
	SupportEmail string `json:"supportEmail,omitempty"` //nolint:tagliatelle // frontend configuration is camelCase
	SupportURL   string `json:"supportUrl,omitempty"`   //nolint:tagliatelle // frontend configuration is camelCase
	TermsURL     string `json:"termsUrl,omitempty"`     //nolint:tagliatelle // frontend configuration is camelCase
	PrivacyURL   string `json:"privacyUrl,omitempty"`   //nolint:tagliatelle // frontend configuration is camelCase
}

// NewBrand validates the configured identity. An empty name falls back to
// DefaultBrandName; every other field is optional. URLs must be absolute
// https so a message or the portal never links to plain-text endpoints.
func NewBrand(in Brand) (Brand, error) {
	out := Brand{
		Name:         strings.TrimSpace(in.Name),
		Tagline:      strings.TrimSpace(in.Tagline),
		LogoURL:      strings.TrimSpace(in.LogoURL),
		FaviconURL:   strings.TrimSpace(in.FaviconURL),
		AccentColor:  strings.TrimSpace(in.AccentColor),
		SupportEmail: strings.TrimSpace(in.SupportEmail),
		SupportURL:   strings.TrimSpace(in.SupportURL),
		TermsURL:     strings.TrimSpace(in.TermsURL),
		PrivacyURL:   strings.TrimSpace(in.PrivacyURL),
	}
	if out.Name == "" {
		out.Name = DefaultBrandName
	}

	const maxText = 120
	for field, value := range map[string]string{"name": out.Name, "tagline": out.Tagline} {
		if len([]rune(value)) > maxText {
			return Brand{}, fmt.Errorf("brand.%s %w", field, errBrandText)
		}
	}

	for field, value := range map[string]string{
		"logoUrl": out.LogoURL, "faviconUrl": out.FaviconURL, "supportUrl": out.SupportURL,
		"termsUrl": out.TermsURL, "privacyUrl": out.PrivacyURL,
	} {
		if value != "" && !httpsURL(value) {
			return Brand{}, fmt.Errorf("brand.%s %w", field, errBrandURL)
		}
	}

	if out.AccentColor != "" && !hexColor.MatchString(out.AccentColor) {
		return Brand{}, fmt.Errorf("brand.accentColor %w", errBrandColor)
	}

	if out.SupportEmail != "" {
		if address, err := mail.ParseAddress(out.SupportEmail); err != nil || address.Address != out.SupportEmail {
			return Brand{}, fmt.Errorf("brand.supportEmail %w", errBrandEmail)
		}
	}

	return out, nil
}

// Origins lists the distinct origins of the brand's images, for the
// portal's Content-Security-Policy.
func (b Brand) Origins() []string {
	var origins []string

	for _, value := range []string{b.LogoURL, b.FaviconURL} {
		if value == "" {
			continue
		}

		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}

		origin := parsed.Scheme + "://" + parsed.Host
		if !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}

	return origins
}

func httpsURL(value string) bool {
	parsed, err := url.Parse(value)

	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		!strings.ContainsAny(value, " \"'<>\\")
}
