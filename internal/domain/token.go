package domain

import (
	"time"

	"github.com/google/uuid"
)

// Token is an authentication token. Only the digest is persisted; the raw
// secret exists once, in the creation response.
type Token struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	Digest    string
	BearerID  uuid.UUID
	// BearerType is the polymorphic bearer: "User", "Product" or
	// "License", stored like Keygen's bearer_type column.
	BearerType string
	BearerRole Role
	Kind       string
	Name       *string
	// Expiry is nil for tokens that never expire.
	Expiry      *time.Time
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Expired reports whether the token's expiry lies in the past.
func (t Token) Expired(now time.Time) bool {
	return t.Expiry != nil && t.Expiry.Before(now)
}
