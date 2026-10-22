package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
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

// The license statuses, in resolution order.
const (
	LicenseStatusActive    = "ACTIVE"
	LicenseStatusInactive  = "INACTIVE"
	LicenseStatusExpiring  = "EXPIRING"
	LicenseStatusExpired   = "EXPIRED"
	LicenseStatusSuspended = "SUSPENDED"
	LicenseStatusBanned    = "BANNED"
)

// LicenseAuth is the slice of a license needed to authenticate by key:
// the license row plus the owner-ban and policy-strategy answers.
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

// License is a license: its row plus the policy it delegates to (the
// reference delegates scheme/strict/floating/limits/check-in straight
// to the policy, so the scenario loads it together with the row).
type License struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	ProductID uuid.UUID
	PolicyID  uuid.UUID
	// OwnerID is the owner user (user_id); Valid false when unowned.
	OwnerID uuid.NullUUID
	GroupID uuid.NullUUID
	Name    *string
	// Key is the stored key: the plaintext for every scheme but
	// LEGACY_ENCRYPT (which stores a bcrypt digest and renders the raw
	// key only in the create response, via RawKey).
	Key string
	// RawKey carries the one-time plaintext of a legacy-encrypted key
	// (the create response renders it; later reads omit key entirely).
	RawKey *string
	// Expiry is nil for perpetual licenses.
	Expiry    *time.Time
	Uses      int32
	Suspended bool
	// Protected is the license override; nil inherits the policy.
	Protected *bool
	Metadata  map[string]any

	// The per-license limit overrides (nil inherits the policy).
	MaxMachinesOverride  *int32
	MaxProcessesOverride *int32
	MaxUsersOverride     *int32
	MaxCoresOverride     *int32
	MaxMemoryOverride    *int64
	MaxDiskOverride      *int64
	MaxUsesOverride      *int32

	// Denormalized counters, kept by the app like the reference's
	// counter caches.
	MachinesCount       int32
	MachinesCoreCount   int32
	MachinesMemoryCount int64
	MachinesDiskCount   int64
	LicenseUsersCount   int32

	LastValidatedAt       *time.Time
	LastValidatedChecksum *string
	LastValidatedVersion  *string
	LastCheckInAt         *time.Time
	LastCheckOutAt        *time.Time
	CreatedAt, UpdatedAt  time.Time

	// Policy is the owning policy row (loaded with the license).
	Policy Policy
	// OwnerBanned: the owner's ban state (license.banned?).
	OwnerBanned bool
	// OwnerEmail of the owner (feeds the generated seed keys and the
	// user-scope check); nil when unowned.
	OwnerEmail *string
}

// MaxMachines answers the effective machine limit (override else
// policy).
func (l License) MaxMachines() *int32 {
	if l.MaxMachinesOverride != nil {
		return l.MaxMachinesOverride
	}

	return l.Policy.MaxMachines
}

// MaxProcesses answers the effective process limit.
func (l License) MaxProcesses() *int32 {
	if l.MaxProcessesOverride != nil {
		return l.MaxProcessesOverride
	}

	return l.Policy.MaxProcesses
}

// MaxUsers answers the effective user limit.
func (l License) MaxUsers() *int32 {
	if l.MaxUsersOverride != nil {
		return l.MaxUsersOverride
	}

	return l.Policy.MaxUsers
}

// MaxCores answers the effective core limit.
func (l License) MaxCores() *int32 {
	if l.MaxCoresOverride != nil {
		return l.MaxCoresOverride
	}

	return l.Policy.MaxCores
}

// MaxMemory answers the effective memory limit.
func (l License) MaxMemory() *int64 {
	if l.MaxMemoryOverride != nil {
		return l.MaxMemoryOverride
	}

	return l.Policy.MaxMemory
}

// MaxDisk answers the effective disk limit.
func (l License) MaxDisk() *int64 {
	if l.MaxDiskOverride != nil {
		return l.MaxDiskOverride
	}

	return l.Policy.MaxDisk
}

// MaxUses answers the effective usage limit.
func (l License) MaxUses() *int32 {
	if l.MaxUsesOverride != nil {
		return l.MaxUsesOverride
	}

	return l.Policy.MaxUses
}

// UsersCount answers the users count (licensees plus the owner).
func (l License) UsersCount() int32 {
	count := l.LicenseUsersCount
	if l.OwnerID.Valid {
		count++
	}

	return count
}

// ProtectedFlag answers protected?: the override when set, else the
// policy's.
func (l License) ProtectedFlag() bool {
	if l.Protected != nil {
		return *l.Protected
	}

	return l.Policy.Protected != nil && *l.Policy.Protected
}

// Banned answers license.banned?: the owner's ban state.
func (l License) Banned() bool {
	return l.OwnerID.Valid && l.OwnerBanned
}

// Expired answers expired?: expiry in the past.
func (l License) Expired(now time.Time) bool {
	return l.Expiry != nil && l.Expiry.Before(now)
}

// Expiring answers expiring?: expiry within the next three days.
func (l License) Expiring(now time.Time) bool {
	return l.Expiry != nil && l.Expiry.After(now) && l.Expiry.Before(now.Add(3*24*time.Hour))
}

// Active answers active?: created or used within t.
func (l License) Active(t time.Time) bool {
	return !l.CreatedAt.Before(t) ||
		(l.LastValidatedAt != nil && !l.LastValidatedAt.Before(t)) ||
		(l.LastCheckOutAt != nil && !l.LastCheckOutAt.Before(t)) ||
		(l.LastCheckInAt != nil && !l.LastCheckInAt.Before(t))
}

// Status resolves the wire status (BANNED → SUSPENDED → EXPIRED →
// EXPIRING → INACTIVE → ACTIVE).
func (l License) Status(now time.Time) string {
	switch {
	case l.Banned():
		return LicenseStatusBanned
	case l.Suspended:
		return LicenseStatusSuspended
	case l.Expired(now):
		return LicenseStatusExpired
	case l.Expiring(now):
		return LicenseStatusExpiring
	case !l.Active(now.Add(-90 * 24 * time.Hour)):
		return LicenseStatusInactive
	default:
		return LicenseStatusActive
	}
}

// CheckInOverdue answers check_in_overdue?.
func (l License) CheckInOverdue(now time.Time) bool {
	if !l.Policy.RequireCheckIn || l.Policy.CheckInInterval == "" ||
		l.Policy.CheckInIntervalCount == nil || l.LastCheckInAt == nil {
		return false
	}

	return l.LastCheckInAt.Before(now.Add(-checkInWindow(l.Policy)))
}

// NextCheckIn answers next_check_in_at (nil unless check-in applies
// and has happened once).
func (l License) NextCheckIn() *time.Time {
	if !l.Policy.RequireCheckIn || l.Policy.CheckInInterval == "" ||
		l.Policy.CheckInIntervalCount == nil || l.LastCheckInAt == nil {
		return nil
	}

	next := l.LastCheckInAt.Add(checkInWindow(l.Policy))

	return &next
}

// CheckInWindow answers the check-in window of a policy (interval ×
// count).
func checkInWindow(policy Policy) time.Duration {
	count := int32(1)
	if policy.CheckInIntervalCount != nil {
		count = *policy.CheckInIntervalCount
	}

	switch policy.CheckInInterval {
	case "day":
		return time.Duration(count) * 24 * time.Hour
	case "week":
		return time.Duration(count) * 7 * 24 * time.Hour
	case "month":
		return time.Duration(count) * 30 * 24 * time.Hour
	case "year":
		return time.Duration(count) * 365 * 24 * time.Hour
	default:
		return 0
	}
}

// RequiresCheckIn answers requires_check_in? (the policy's flag).
func (p Policy) RequiresCheckIn() bool {
	return p.RequireCheckIn
}

// CompareLegacyKey checks a raw legacy-encrypted key against the
// stored bcrypt digest (BCrypt(SHA256hex(raw))).
func CompareLegacyKey(storedDigest, raw string) bool {
	return crypto.CompareLegacyKey(storedDigest, raw)
}
