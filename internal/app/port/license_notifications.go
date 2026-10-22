package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// LicenseNotificationCandidate is a license id with its event throttle state.
type LicenseNotificationCandidate struct {
	ID    uuid.UUID
	State domain.LicenseNotificationState
}

// LicenseNotifications is the scheduled license event storage port.
type LicenseNotifications interface {
	TryLicenseNotificationLock(ctx context.Context) (bool, error)
	LicenseNotificationCandidates(
		ctx context.Context, accountID uuid.UUID, now time.Time,
	) ([]LicenseNotificationCandidate, error)
	LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.License, error)
	StampLicenseNotification(ctx context.Context, accountID, id uuid.UUID, event string, now time.Time) error
}
