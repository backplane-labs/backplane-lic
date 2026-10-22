package domain

import (
	"time"

	"github.com/google/uuid"
)

// ReservedKeys: the two route-segment-colliding key values.
var ReservedKeys = []string{"actions", "action"} //nolint:gochecknoglobals // transcribed reference data

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
