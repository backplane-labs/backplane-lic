package licensing

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ErrConfig reports an invalid Config to New.
var ErrConfig = errors.New("invalid config")

// Defaults the client falls back to when Config leaves a field empty.
const (
	// DefaultAPIURL is the Keygen cloud API endpoint.
	DefaultAPIURL = "https://api.keygen.sh"
	// DefaultAPIPrefix is the major version prefix of every API path.
	DefaultAPIPrefix = "/v1"
	// DefaultAPIVersion is sent as Keygen-Version on every request.
	DefaultAPIVersion = "1.8"
	// DefaultClockSkew is the maximum tolerated difference between the
	// server's Date/issued timestamps and the local clock. Negative
	// Config.ClockSkew disables the checks.
	DefaultClockSkew = 5 * time.Minute
)

// Config configures a Client. The zero value plus Account and LicenseKey
// (or Token) is a valid starting point; everything else falls back to the
// Keygen cloud defaults.
type Config struct {
	// APIURL is the API origin, e.g. https://api.keygen.sh (default) or
	// http://localhost:3900 for a self-hosted instance. When it equals
	// the Keygen cloud URL, requests are scoped under
	// /accounts/{Account}; any other origin is used as-is (singleplayer
	// / custom domain form).
	APIURL string

	// APIPrefix prefixes every path. Default "/v1".
	APIPrefix string

	// APIVersion is sent as the Keygen-Version header. Default "1.8".
	APIVersion string

	// Account is the Keygen account id or slug. Required for the Keygen
	// cloud URL; ignored for self-hosted singleplayer endpoints.
	Account string

	// Product scopes license validations to a product id (optional).
	Product string

	// LicenseKey authenticates as the license ("License" scheme).
	LicenseKey string

	// Token authenticates with an API token ("Bearer" scheme). At most
	// one of LicenseKey and Token should be set; LicenseKey wins.
	Token string

	// PublicKey verifies API response signatures, license/machine file
	// signatures, and signed license keys: the account's ed25519 public
	// key as hex, or a PEM-encoded RSA or ECDSA public key. Optional —
	// without it responses and files are trusted as-is.
	PublicKey string

	// UserAgent is appended to the client's default User-Agent.
	UserAgent string

	// HTTPClient performs the requests. Default: a pooled client with
	// sane timeouts.
	HTTPClient *http.Client

	// Logger receives request/response debug lines. Optional.
	Logger *slog.Logger

	// Headers are extra headers sent on every request, e.g. Host or
	// X-Forwarded-Proto for instances behind a proxy. The "Host" key
	// sets the request's Host header (URL and signatures follow).
	Headers map[string]string

	// ClockSkew bounds the tolerated difference between the server's
	// clocks and ours when checking response Date headers and file
	// issued timestamps. Default 5m; negative disables.
	ClockSkew time.Duration
}

// normalize fills defaults and validates the combination.
func (c Config) normalize() (Config, error) {
	if c.APIURL == "" {
		c.APIURL = DefaultAPIURL
	}

	if !strings.HasPrefix(c.APIURL, "http://") && !strings.HasPrefix(c.APIURL, "https://") {
		c.APIURL = "https://" + c.APIURL
	}

	c.APIURL = strings.TrimSuffix(c.APIURL, "/")

	if c.APIPrefix == "" {
		c.APIPrefix = DefaultAPIPrefix
	}

	if c.APIPrefix[0] != '/' {
		c.APIPrefix = "/" + c.APIPrefix
	}

	if c.APIVersion == "" {
		c.APIVersion = DefaultAPIVersion
	}

	if c.ClockSkew == 0 {
		c.ClockSkew = DefaultClockSkew
	}

	if c.Account == "" && c.APIURL == DefaultAPIURL {
		return Config{}, fmt.Errorf("%w: account is required for the Keygen cloud API", ErrConfig)
	}

	if c.LicenseKey == "" && c.Token == "" {
		return Config{}, fmt.Errorf("%w: licenseKey or token must be set", ErrConfig)
	}

	return c, nil
}

// cloudScoped reports whether requests need the /accounts/{account} prefix.
func (c Config) cloudScoped() bool {
	return c.APIURL == DefaultAPIURL
}
