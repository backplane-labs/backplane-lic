package domain

import (
	"time"

	"github.com/google/uuid"
)

// Group groups users, licenses and machines under shared limits.
type Group struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	Name      string
	// MaxUsers/MaxLicenses/MaxMachines are nil without a limit.
	MaxUsers    *int
	MaxLicenses *int
	MaxMachines *int
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupOwner is the group↔user ownership join resource.
type GroupOwner struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	GroupID   uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}
