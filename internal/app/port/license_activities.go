package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// LicenseActivities is the admin application surface offered to wiring.
type LicenseActivities interface {
	CreateLicense(ctx context.Context, in CreateLicenseIn, key string) (LicenseOut, error)
	GetLicense(ctx context.Context, in LicenseIn, key string) (LicenseOut, error)
	SuspendLicense(ctx context.Context, in LicenseIn, key string) (LicenseOut, error)
	RenewLicense(ctx context.Context, in LicenseIn, key string) (LicenseOut, error)
	ValidateKey(ctx context.Context, in ValidateKeyIn, key string) (ValidateKeyOut, error)
	IncrementUsage(ctx context.Context, in IncrementUsageIn, key string) (LicenseOut, error)
}

type CreateLicenseIn struct {
	PolicyID uuid.UUID      `json:"policy_id"`
	OwnerID  *uuid.UUID     `json:"owner_id,omitempty"`
	Name     *string        `json:"name,omitempty"`
	Key      *string        `json:"key,omitempty"      schemapb:"secret=true"`
	Expiry   *time.Time     `json:"expiry,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type LicenseIn struct {
	License string `json:"license" schemapb:"description=License UUID or key"`
}

type IncrementUsageIn struct {
	License   string `json:"license"`
	Increment int64  `json:"increment"`
}

type ValidateKeyIn struct {
	Key          string   `json:"key"                    schemapb:"secret=true"`
	Product      *string  `json:"product,omitempty"`
	Policy       *string  `json:"policy,omitempty"`
	Fingerprints []string `json:"fingerprints,omitempty"`
	Entitlements []string `json:"entitlements,omitempty"`
}

type LicenseOut struct {
	License LicenseEvent `json:"license"`
	Key     string       `json:"key"     schemapb:"secret=true"`
	Name    string       `json:"name"`
}

type ValidateKeyOut struct {
	License *LicenseOut `json:"license"`
	Valid   bool        `json:"valid"`
	Code    string      `json:"code"`
	Detail  string      `json:"detail"`
}

// ActivityReceipts persists the result with the mutation, under an xact lock.
// Receipts intentionally have no TTL: an old activity retry must not mutate twice.
type ActivityReceipts interface {
	Do(ctx context.Context, fn func(context.Context) error) error
	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
	LockAutomationActivity(ctx context.Context, accountID uuid.UUID, key string) error
	AutomationActivityResult(ctx context.Context, accountID uuid.UUID, key string) (ActivityReceipt, error)
	SaveAutomationActivityResult(ctx context.Context, accountID uuid.UUID, key string, receipt ActivityReceipt) error
}

type ActivityReceipt struct {
	RequestHash string
	Result      json.RawMessage
}
