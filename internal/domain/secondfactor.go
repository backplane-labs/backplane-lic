package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The TOTP provisioning constants of the reference's SecondFactor model.
const (
	SecondFactorIssuer = "Keygen"
	SecondFactorImage  = "https://keygen.sh/images/logomark.png"
)

// SecondFactor is a user's TOTP second factor. At most one exists per
// user (unique index), exactly like the reference.
type SecondFactor struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	UserID    uuid.UUID
	// Secret is the base32 TOTP secret.
	Secret  string
	Enabled bool
	// LastVerifiedAt blocks OTP replays (the matched timestamp is
	// persisted after every successful verification).
	LastVerifiedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// URI is the provisioning URI, rendered only while the factor is
// disabled (the secret is not exposed once enabled).
func (s SecondFactor) URI(email string) string {
	label := SecondFactorIssuer + ":" + encodeURIComponent(email)

	return fmt.Sprintf(
		"otpauth://totp/%s?secret=%s&image=%s&issuer=%s",
		label, s.Secret, url.QueryEscape(SecondFactorImage), SecondFactorIssuer,
	)
}

// encodeURIComponent percent-encodes everything but the unreserved
// characters, like the reference's ERB::Util.url_encode (Go's
// PathEscape leaves sub-delims like "@" and ":" alone).
func encodeURIComponent(s string) string {
	var b strings.Builder

	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			_, _ = fmt.Fprintf(&b, "%%%02X", c)
		}
	}

	return b.String()
}
