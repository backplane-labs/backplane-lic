package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// TryLicenseNotificationLock shares the periodic jobs' transaction lock.
func (r LicensesRepo) TryLicenseNotificationLock(ctx context.Context) (bool, error) {
	row, err := r.q.TryJobsAdvisoryLock(ctx)
	if err != nil {
		return false, fmt.Errorf("postgres: license notification lock: %w", err)
	}

	return row.Locked != nil && *row.Locked, nil
}

// LicenseNotificationCandidates narrows the scan to the reference's windows.
func (r LicensesRepo) LicenseNotificationCandidates(
	ctx context.Context, accountID uuid.UUID, now time.Time,
) ([]port.LicenseNotificationCandidate, error) {
	rows, err := r.q.LicenseNotificationCandidates(ctx, db.LicenseNotificationCandidatesParams{
		AccountID: accountID, Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: license notification candidates: %w", err)
	}

	out := make([]port.LicenseNotificationCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, port.LicenseNotificationCandidate{
			ID: row.ID,
			State: domain.LicenseNotificationState{
				Expiration: row.LastExpirationEventSentAt, ExpiringSoon: row.LastExpiringSoonEventSentAt,
				CheckInOverdue: row.LastCheckInEventSentAt, CheckInSoon: row.LastCheckInSoonEventSentAt,
			},
		})
	}

	return out, nil
}

// StampLicenseNotification persists the throttle in the caller's transaction.
func (r LicensesRepo) StampLicenseNotification(
	ctx context.Context, accountID, id uuid.UUID, event string, now time.Time,
) error {
	if err := r.q.StampLicenseNotification(ctx, db.StampLicenseNotificationParams{
		AccountID: accountID, ID: id, Event: event, Now: now,
	}); err != nil {
		return fmt.Errorf("postgres: stamp license notification: %w", err)
	}

	return nil
}
