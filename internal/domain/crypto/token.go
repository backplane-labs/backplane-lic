// Token secrets: the v3 format, its digests and the role-derived
// prefixes. Only v3 exists here (the current algorithm; v1/v2 are legacy
// and authenticate as invalid).
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// AlgoVersion is the current token secret version suffix.
const AlgoVersion = "v3"

// Secret bytes of generated tokens: 32 for every token, 16 for activation
// tokens (hex-encoded to 64 and 32 characters).
const (
	tokenSecretBytes    = 32
	activationTokenBits = 16
)

// The bearer role names the prefix/kind maps are keyed by (the domain
// Role type cannot be imported here: domain imports this package).
const (
	roleAdmin   = "admin"
	roleUser    = "user"
	roleLicense = "license"
)

// uuidDashPos marks the dash positions of a canonical UUID string.
var uuidDashPos = //nolint:gochecknoglobals // fixed positions of the canonical UUID form
map[int]struct{}{8: {}, 13: {}, 18: {}, 23: {}}

// versionRe mirrors TokenLookupService's TOKEN_VERSION_RE: any value
// ending in a v<digits> version marker is a versioned token value.
var versionRe = regexp.MustCompile(`\A.+?(v\d+)\z`)

// TokenKind maps a bearer role to the token kind string stored on the
// row and rendered on the wire.
func TokenKind(role string) string {
	switch role {
	case roleAdmin:
		return "admin-token"
	case "developer":
		return "developer-token"
	case "sales_agent":
		return "sales-token"
	case "support_agent":
		return "support-token"
	case "read_only":
		return "read-only-token"
	case roleUser:
		return "user-token"
	case "product":
		return "product-token"
	case roleLicense:
		return "activation-token"
	default:
		return "token"
	}
}

// TokenPrefix maps a bearer role to the secret's prefix block.
func TokenPrefix(role string) string {
	switch role {
	case roleAdmin:
		return roleAdmin
	case "developer":
		return "dev"
	case "sales_agent":
		return "sales"
	case "support_agent":
		return "spprt"
	case "read_only":
		return "read"
	case roleUser:
		return roleUser
	case "product":
		return "prod"
	case roleLicense:
		return "activ"
	default:
		return "token"
	}
}

// GenerateTokenSecret generates a raw v3 secret for the bearer's role:
// "<prefix>-<hex>v3". Activation tokens use 16 random bytes, everything
// else 32.
func GenerateTokenSecret(rng io.Reader, role string) (string, error) {
	n := tokenSecretBytes
	if role == roleLicense {
		n = activationTokenBits
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(rng, buf); err != nil {
		return "", fmt.Errorf("crypto: token secret: %w", err)
	}

	return TokenPrefix(role) + "-" + hex.EncodeToString(buf) + AlgoVersion, nil
}

// DigestV3 computes the stored digest of a raw v3 secret:
// HMAC-SHA256 over the account's hex secret key (the key is the hex
// string's bytes, like the reference's HMAC).
func DigestV3(secretKeyHex, raw string) string {
	mac := hmac.New(sha256.New, []byte(secretKeyHex))
	mac.Write([]byte(raw))

	return hex.EncodeToString(mac.Sum(nil))
}

// Versioned reports whether the value carries a trailing v<digits>
// version marker (any version; only v3 verifies).
func Versioned(value string) bool {
	return versionRe.MatchString(value)
}

// UUIDShaped reports whether the value looks like a UUID — the mistake
// TokenFormatInvalid exists for.
func UUIDShaped(value string) bool {
	if len(value) != 36 || strings.Count(value, "-") != 4 {
		return false
	}

	for i, c := range value {
		_, isDash := uuidDashPos[i]

		switch {
		case isDash:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}

	return true
}
