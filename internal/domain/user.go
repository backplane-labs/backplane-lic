package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// User is an account's user: a license owner or an admin bearer.
type User struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	FirstName string
	LastName  string
	Email     string
	// PasswordDigest is the bcrypt hash; empty for passwordless users.
	PasswordDigest string
	Role           Role
	// Permissions overrides the role's default set when non-empty (an EE
	// feature; kept for wire fidelity of the storage shape).
	Permissions map[string]struct{}
	GroupID     uuid.NullUUID
	Metadata    map[string]any
	// PasswordResetToken is the HMAC digest of the outstanding
	// password-reset token (plaintext only in the webhook/email).
	PasswordResetToken  string
	PasswordResetSentAt *time.Time
	// BannedAt is set when the user is banned; nil otherwise.
	BannedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Banned reports whether the user is banned.
func (u User) Banned() bool {
	return u.BannedAt != nil
}

// HasPassword reports whether the user may authenticate with a password.
func (u User) HasPassword() bool {
	return u.PasswordDigest != ""
}

// FullName is nil on the wire unless both names are present, like the
// Keygen serializer.
func (u User) FullName() *string {
	if u.FirstName == "" || u.LastName == "" {
		return nil
	}

	name := u.FirstName + " " + u.LastName

	return &name
}

// Status computes the user's wire status: BANNED beats everything, then
// ACTIVE when created within the last 90 days or holding an active
// license, else INACTIVE.
func (u User) Status(activeLicense bool, now time.Time) string {
	if u.Banned() {
		return "BANNED"
	}

	if u.CreatedAt.After(now.AddDate(0, 0, -90)) || activeLicense {
		return "ACTIVE"
	}

	return "INACTIVE"
}

// Authenticates reports whether the password matches the digest
// (bcrypt, like the reference's has_secure_password).
func (u User) Authenticates(password string) bool {
	return u.HasPassword() && crypto.PasswordMatches(u.PasswordDigest, password)
}

// PasswordReset is the password-reset event's user and one-time plaintext claim.
// The claim belongs only to webhook document metadata, never user attributes.
type PasswordReset struct {
	User  User
	Token string
}
