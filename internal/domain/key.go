package domain

import (
	"time"

	"github.com/google/uuid"
)

// The route segments that collide with an id.
const (
	reservedActions = "actions"
	reservedAction  = "action"
)

// ReservedKeys: the two route-segment-colliding key values.
var ReservedKeys = []string{reservedActions, reservedAction} //nolint:gochecknoglobals // transcribed reference data

// Key is a member of a pooled policy's key pool.
type Key struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	PolicyID  uuid.UUID
	// ProductID is the key's policy's product (rendered in the key
	// document's product relationship).
	ProductID            uuid.UUID
	Key                  string
	CreatedAt, UpdatedAt time.Time
}
