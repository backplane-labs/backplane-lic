package licenses

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Notifier emits scheduled license events. The SDK composition owns ticking;
// persisted per-event timestamps, not process memory, enforce daily throttles.
type Notifier struct {
	store     port.LicenseNotifications
	events    events.Publisher
	accountID uuid.UUID
	clock     func() time.Time
	doTx      func(context.Context, func(context.Context) error) error
}

func NewNotifier(
	store port.LicenseNotifications, publisher events.Publisher, accountID uuid.UUID,
	clock func() time.Time, doTx func(context.Context, func(context.Context) error) error,
) Notifier {
	return Notifier{store: store, events: publisher, accountID: accountID, clock: clock, doTx: doTx}
}

// Tick commits notification stamps and their outbox messages together.
func (n Notifier) Tick(ctx context.Context) error {
	return n.doTx(ctx, func(ctx context.Context) error {
		locked, err := n.store.TryLicenseNotificationLock(ctx)
		if err != nil {
			return fmt.Errorf("licenses: notification lock: %w", err)
		}

		if !locked {
			return nil
		}

		now := n.clock()

		candidates, err := n.store.LicenseNotificationCandidates(ctx, n.accountID, now)
		if err != nil {
			return fmt.Errorf("licenses: notification candidates: %w", err)
		}

		for _, candidate := range candidates {
			license, err := n.store.LicenseByID(ctx, n.accountID, candidate.ID)
			if err != nil {
				return fmt.Errorf("licenses: notification license: %w", err)
			}

			for _, event := range domain.LicenseNotifications(license, candidate.State, now) {
				if err := n.events.Publish(ctx, event, license); err != nil {
					return fmt.Errorf("licenses: notification publish: %w", err)
				}

				if err := n.store.StampLicenseNotification(ctx, n.accountID, license.ID, event, now); err != nil {
					return fmt.Errorf("licenses: notification stamp: %w", err)
				}
			}
		}

		return nil
	})
}
