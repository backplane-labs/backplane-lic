package domain

import (
	"time"

	"github.com/google/uuid"
)

// Heartbeat defaults (Machine::HEARTBEAT_TTL and HEARTBEAT_DRIFT).
const (
	HeartbeatTTLSeconds   int32 = 600
	HeartbeatDriftSeconds int32 = 30
)

// Heartbeat statuses of machines and processes.
const (
	MachineNotStarted    = "NOT_STARTED"
	MachineAlive         = "ALIVE"
	MachineDead          = "DEAD"
	HeartbeatResurrected = "RESURRECTED"
)

// Policy strategy values the machine rules branch on.
const (
	basisFromCreation   = "FROM_CREATION"
	reviveNever         = "NO_REVIVE"
	reviveAlways        = "ALWAYS_REVIVE"
	cullDeactivate      = "DEACTIVATE_DEAD"
	leasePerUser        = "PER_USER"
	leasePerMachine     = "PER_MACHINE"
	uniquePerLicense    = "UNIQUE_PER_LICENSE"
	uniquePerMachineVal = "UNIQUE_PER_MACHINE"
)

// Resurrection windows of the N_MINUTE_REVIVE strategies.
const (
	lazarus15 = 15 * time.Minute
	lazarus10 = 10 * time.Minute
	lazarus5  = 5 * time.Minute
	lazarus2  = 2 * time.Minute
)

// Machine is a licensed device.
type Machine struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	LicenseID   uuid.UUID
	PolicyID    uuid.UUID
	OwnerID     uuid.NullUUID
	GroupID     uuid.NullUUID
	Name        *string
	Fingerprint string
	IP          *string
	Hostname    *string
	Platform    *string
	Cores       *int32
	Memory      *int64
	Disk        *int64
	// MaxProcessesOverride is the machine's own process limit (else
	// the license's maxProcesses).
	MaxProcessesOverride *int32
	Metadata             map[string]any
	LastHeartbeatAt      *time.Time
	LastCheckOutAt       *time.Time
	LastDeathEventSentAt *time.Time
	CreatedAt, UpdatedAt time.Time

	// The facts joined for authorization: the product the machine's
	// policy belongs to, the license's owner and its resolved protected
	// flag.
	ProductID        uuid.UUID
	LicenseOwnerID   uuid.NullUUID
	LicenseProtected bool
	// LicenseMaxProcesses is the license's process limit (its override
	// else the policy's).
	LicenseMaxProcesses *int32

	// Heartbeat is the policy's heartbeat rule set.
	Heartbeat HeartbeatPolicy
	// HeartbeatStatus is Status at load time, or RESURRECTED right after
	// a resurrection (the reference's transient status override).
	HeartbeatStatus string
}

// HeartbeatPolicy is the heartbeat part of a policy.
type HeartbeatPolicy struct {
	Require     bool
	Duration    *int32
	Basis       string
	Resurrect   string
	CullOnDeath string
}

// HeartbeatOf is the heartbeat rule set of the policy.
func (p Policy) HeartbeatOf() HeartbeatPolicy {
	return HeartbeatPolicy{
		Require: p.RequireHeartbeat, Duration: p.HeartbeatDuration, Basis: p.HeartbeatBasis,
		Resurrect: p.HeartbeatResurrectionStrategy, CullOnDeath: p.HeartbeatCullStrategy,
	}
}

// DurationOrDefault is the policy's heartbeat duration, else the TTL.
func (h HeartbeatPolicy) DurationOrDefault() time.Duration {
	return time.Duration(h.DurationSeconds()) * time.Second
}

// DurationSeconds is DurationOrDefault in whole seconds.
func (h HeartbeatPolicy) DurationSeconds() int32 {
	if h.Duration != nil {
		return *h.Duration
	}

	return HeartbeatTTLSeconds
}

// FromCreation answers heartbeat_from_creation?: the clock starts at
// activation instead of at the first ping.
func (h HeartbeatPolicy) FromCreation() bool { return h.Basis == basisFromCreation }

// ResurrectDead answers resurrect_dead?: an unset strategy never revives.
func (h HeartbeatPolicy) ResurrectDead() bool {
	return h.Resurrect != "" && h.Resurrect != reviveNever
}

// AlwaysResurrectDead answers always_resurrect_dead?.
func (h HeartbeatPolicy) AlwaysResurrectDead() bool { return h.Resurrect == reviveAlways }

// LazarusTTL is the window after death within which a ping revives.
func (h HeartbeatPolicy) LazarusTTL() time.Duration {
	switch h.Resurrect {
	case "15_MINUTE_REVIVE":
		return lazarus15
	case "10_MINUTE_REVIVE":
		return lazarus10
	case "5_MINUTE_REVIVE":
		return lazarus5
	case "2_MINUTE_REVIVE":
		return lazarus2
	case "1_MINUTE_REVIVE":
		return time.Minute
	default:
		return 0
	}
}

// DeactivateDead answers deactivate_dead?: an unset strategy culls.
func (h HeartbeatPolicy) DeactivateDead() bool {
	return h.CullOnDeath == "" || h.CullOnDeath == cullDeactivate
}

// RequiresHeartbeat answers requires_heartbeat?: the policy demands it
// or the machine has pinged once.
func (m Machine) RequiresHeartbeat() bool {
	return m.Heartbeat.Require || m.LastHeartbeatAt != nil
}

// NextHeartbeatAt is when the next ping is due; nil before the first.
func (m Machine) NextHeartbeatAt() *time.Time {
	if m.LastHeartbeatAt == nil {
		return nil
	}

	next := m.LastHeartbeatAt.Add(m.Heartbeat.DurationOrDefault())

	return &next
}

// Status is Machine#heartbeat_status without the override.
func (m Machine) Status(now time.Time) string {
	next := m.NextHeartbeatAt()

	switch {
	case next == nil && (!m.RequiresHeartbeat() || !m.CreatedAt.Before(now.Add(-m.Heartbeat.DurationOrDefault()))):
		return MachineNotStarted
	case next != nil && !next.Before(now):
		return MachineAlive
	default:
		return MachineDead
	}
}

// Alive answers heartbeat_alive?: alive or just resurrected.
func (m Machine) Alive() bool {
	return m.HeartbeatStatus == MachineAlive || m.HeartbeatStatus == HeartbeatResurrected
}

// NotStarted answers heartbeat_not_started?.
func (m Machine) NotStarted() bool { return m.HeartbeatStatus == MachineNotStarted }

// InAliveScope answers the reference's Machine.alive SQL scope (what
// license validation counts): every machine not dead — not yet started
// ones included, unlike the alive? predicate.
func (m Machine) InAliveScope() bool { return m.HeartbeatStatus != MachineDead }

// ResurrectionPeriodPassed answers resurrection_period_passed?.
func (m Machine) ResurrectionPeriodPassed(now time.Time) bool {
	if m.Heartbeat.AlwaysResurrectDead() {
		return false
	}

	next := m.NextHeartbeatAt()
	if !m.RequiresHeartbeat() || !m.Heartbeat.ResurrectDead() || next == nil {
		return true
	}

	return now.After(next.Add(m.Heartbeat.LazarusTTL()))
}

// MaxProcesses answers the machine's effective process limit (the
// override else the license's).
func (m Machine) MaxProcesses() *int32 { return limitOf(m.MaxProcessesOverride, m.LicenseMaxProcesses) }

// WithinOverage answers whether next, past limit, is still allowed by
// the policy's overage strategy (ALWAYS_ALLOW_OVERAGE, or within the
// 1.25x, 1.5x or 2x allowance).
func (p Policy) WithinOverage(next, limit float64) bool {
	switch p.OverageStrategy {
	case "ALWAYS_ALLOW_OVERAGE":
		return true
	case "ALLOW_1_25X_OVERAGE":
		return next <= limit*1.25
	case "ALLOW_1_5X_OVERAGE":
		return next <= limit*1.5
	case "ALLOW_2X_OVERAGE":
		return next <= limit*2
	default:
		return false
	}
}

// MachineLeasePerUser answers machine_lease_per_user?: limits count per
// machine owner instead of per license.
func (p Policy) MachineLeasePerUser() bool { return p.MachineLeasingStrategy == leasePerUser }

// MachineUniqueness is the effective machine uniqueness strategy (an
// unset one is per license).
func (p Policy) MachineUniqueness() string {
	if p.MachineUniquenessStrategy == "" {
		return uniquePerLicense
	}

	return p.MachineUniquenessStrategy
}

// LicenseUser is the license↔licensee join resource.
type LicenseUser struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	LicenseID uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LicenseEntitlement is the license↔entitlement join resource.
type LicenseEntitlement struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	LicenseID     uuid.UUID
	EntitlementID uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MachineComponent is a named hardware fingerprint of a machine.
type MachineComponent struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	MachineID   uuid.UUID
	Name        string
	Fingerprint string
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// ProductID and LicenseID are its machine's (joined).
	ProductID uuid.UUID
	LicenseID uuid.UUID
}

// MachineProcess is a running process on a machine, with a heartbeat.
type MachineProcess struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	MachineID            uuid.UUID
	PID                  string
	Metadata             map[string]any
	LastHeartbeatAt      time.Time
	LastDeathEventSentAt *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	// Heartbeat is its machine policy's heartbeat rule set.
	Heartbeat HeartbeatPolicy
	// HeartbeatStatus is Status at load time, or RESURRECTED right after
	// a resurrection.
	HeartbeatStatus string
	// ProductID and LicenseID are its machine's (joined).
	ProductID uuid.UUID
	LicenseID uuid.UUID
}

// NextHeartbeatAt is when the next ping is due (the machine's interval).
func (p MachineProcess) NextHeartbeatAt() time.Time {
	return p.LastHeartbeatAt.Add(p.Heartbeat.DurationOrDefault())
}

// Status is MachineProcess#status without the override: ALIVE until the
// next heartbeat is past, then DEAD.
func (p MachineProcess) Status(now time.Time) string {
	if !p.NextHeartbeatAt().Before(now) {
		return MachineAlive
	}

	return MachineDead
}

// ResurrectionPeriodPassed answers resurrection_period_passed?.
func (p MachineProcess) ResurrectionPeriodPassed(now time.Time) bool {
	if p.Heartbeat.AlwaysResurrectDead() {
		return false
	}

	if !p.Heartbeat.ResurrectDead() {
		return true
	}

	return now.After(p.NextHeartbeatAt().Add(p.Heartbeat.LazarusTTL()))
}

// ProcessLeasing is the policy's process leasing strategy: PER_MACHINE
// (the default), PER_LICENSE or PER_USER.
func (p Policy) ProcessLeasing() string {
	if p.ProcessLeasingStrategy == "" {
		return leasePerMachine
	}

	return p.ProcessLeasingStrategy
}

// ComponentUniqueness is the effective component uniqueness strategy (an
// unset one is per machine).
func (p Policy) ComponentUniqueness() string {
	if p.ComponentUniquenessStrategy == "" {
		return uniquePerMachineVal
	}

	return p.ComponentUniquenessStrategy
}
