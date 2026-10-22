package domain

import (
	"time"

	"github.com/google/uuid"
)

// HeartbeatTTLSeconds is the default heartbeat duration when the
// policy sets none (Keygen's HEARTBEAT_TTL of 10 minutes).
const HeartbeatTTLSeconds int32 = 600

// Machine heartbeat statuses.
const (
	MachineNotStarted = "NOT_STARTED"
	MachineAlive      = "ALIVE"
	MachineDead       = "DEAD"
)

// Machine is a licensed device. The full machine resource group lands
// next; licenses reach machines as relationship documents and validate
// against their rows (the heartbeat facts ride along).
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
	// HeartbeatStatus is the computed NOT_STARTED/ALIVE/DEAD.
	HeartbeatStatus      string
	RequireHeartbeat     bool
	HeartbeatDuration    *int32
	CreatedAt, UpdatedAt time.Time
}

// Alive answers whether the machine's heartbeat is alive (or
// resurrected).
func (m Machine) Alive() bool { return m.HeartbeatStatus == MachineAlive }

// NotStarted answers whether the machine never started its heartbeat.
func (m Machine) NotStarted() bool { return m.HeartbeatStatus == MachineNotStarted }

// HeartbeatDurationOrDefault answers the effective heartbeat duration.
func (m Machine) HeartbeatDurationOrDefault() int32 {
	if m.HeartbeatDuration != nil {
		return *m.HeartbeatDuration
	}

	return HeartbeatTTLSeconds
}

// MaxProcesses answers the machine's effective process limit (the
// override else the license's).
func (m Machine) MaxProcesses(licenseMax *int32) *int32 {
	if m.MaxProcessesOverride != nil {
		return m.MaxProcessesOverride
	}

	return licenseMax
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
