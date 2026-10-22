package domain

import (
	"time"

	"github.com/google/uuid"
)

// The policy's authentication strategies (Policy#authentication_strategy).
const (
	AuthStrategyToken   = "TOKEN"
	AuthStrategyLicense = "LICENSE"
	AuthStrategySession = "SESSION"
	AuthStrategyMixed   = "MIXED"
	AuthStrategyNone    = "NONE"
)

// The policy's expiration strategies that revoke access on expiry.
const (
	ExpirationRestrictAccess = "RESTRICT_ACCESS"
)

// LicenseAuth is the slice of a license needed to authenticate by key:
// the license row plus the owner-ban and policy-strategy answers. The
// full license resource is a later phase.
type LicenseAuth struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	Key       string
	Suspended bool
	// GroupID is the license's group, when it has one (Valid false
	// otherwise).
	GroupID uuid.NullUUID
	// Expiry is nil when the license never expires.
	Expiry *time.Time
	// OwnerBanned: the owning user is banned (license.banned?).
	OwnerBanned bool
	// PolicyAuthenticationStrategy may be empty (legacy nil → TOKEN).
	PolicyAuthenticationStrategy string
	// PolicyExpirationStrategy may be empty.
	PolicyExpirationStrategy string
}

// SupportsTokenAuth mirrors Policy#supports_token_auth?.
func (l LicenseAuth) SupportsTokenAuth() bool {
	switch l.PolicyAuthenticationStrategy {
	case "", AuthStrategyToken, AuthStrategyMixed:
		return true
	default:
		return false
	}
}

// SupportsLicenseAuth mirrors Policy#supports_license_auth?.
func (l LicenseAuth) SupportsLicenseAuth() bool {
	return l.PolicyAuthenticationStrategy == AuthStrategyLicense ||
		l.PolicyAuthenticationStrategy == AuthStrategyMixed
}

// Expired reports whether the license's expiry lies in the past.
func (l LicenseAuth) Expired(now time.Time) bool {
	return l.Expiry != nil && l.Expiry.Before(now)
}

// RestrictsAccess mirrors Policy#revoke_access? (RESTRICT_ACCESS).
func (l LicenseAuth) RestrictsAccess() bool {
	return l.PolicyExpirationStrategy == ExpirationRestrictAccess
}
