package domain

import (
	"time"

	"github.com/google/uuid"
)

// ReservedEntitlementCodes: the two route-segment-colliding aliases.
var ReservedEntitlementCodes = []string{"actions", "action"} //nolint:gochecknoglobals // transcribed reference data

// Entitlement is an account-wide feature policies and licenses attach.
type Entitlement struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	Name                 string
	Code                 string
	Metadata             map[string]any
	CreatedAt, UpdatedAt time.Time
}

// PolicyEntitlement is one policy_entitlements join row: the resource
// the attach route answers.
type PolicyEntitlement struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	PolicyID      uuid.UUID
	EntitlementID uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
