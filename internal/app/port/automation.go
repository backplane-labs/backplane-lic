package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// DeliverIn is the installation-owned rendering and delivery contract.
// ResetURL contains a credential: keep it out of logs and event payloads.
type DeliverIn struct {
	AccountID uuid.UUID    `json:"account_id"`
	Template  string       `json:"template"`
	Recipient string       `json:"recipient"`
	Locale    string       `json:"locale"`
	Data      DeliveryData `json:"data"`
}

type DeliveryData struct {
	UserID        uuid.UUID `json:"user_id"`
	UserName      string    `json:"user_name"`
	UserEmail     string    `json:"user_email"`
	ResetURL      string    `json:"reset_url"      schemapb:"secret=true"`
	LicenseID     uuid.UUID `json:"license_id"`
	LicenseKey    string    `json:"license_key"    schemapb:"secret=true"`
	LicenseExpiry string    `json:"license_expiry"`
	ProductName   string    `json:"product_name"`
}

type DeliverOut struct{}

// Delivery is a bound hook adapter. Key remains stable across outbox retries.
type Delivery interface {
	Bound() bool
	Deliver(ctx context.Context, in DeliverIn, key string) error
}

// AutomationStore supplies the transaction boundary shared by event and webhook writes.
type AutomationStore interface {
	Do(ctx context.Context, fn func(context.Context) error) error
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
}

// UserEvent intentionally excludes passwords, reset claims and user metadata.
type UserEvent struct {
	AccountID  uuid.UUID `json:"account_id"`
	UserID     uuid.UUID `json:"user_id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	OccurredAt time.Time `json:"occurred_at"`
}

// LicenseEvent is a stable snapshot, not a serialization of the storage model.
type LicenseEvent struct {
	AccountID      uuid.UUID  `json:"account_id"`
	LicenseID      uuid.UUID  `json:"license_id"`
	ProductID      uuid.UUID  `json:"product_id"`
	PolicyID       uuid.UUID  `json:"policy_id"`
	UserID         uuid.UUID  `json:"user_id"`
	Expiry         *time.Time `json:"expiry"`
	Uses           int32      `json:"uses"`
	Suspended      bool       `json:"suspended"`
	ValidationCode string     `json:"validation_code"`
	OccurredAt     time.Time  `json:"occurred_at"`
}
