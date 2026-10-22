package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// JobsRepo adapts the periodic-job queries.
type JobsRepo struct {
	q *db.Queries
}

// TryAdvisoryLock answers whether this transaction won the jobs lock
// (one replica runs a tick).
func (r JobsRepo) TryAdvisoryLock(ctx context.Context) (bool, error) {
	row, err := r.q.TryJobsAdvisoryLock(ctx)
	if err != nil {
		return false, fmt.Errorf("postgres: jobs lock: %w", err)
	}

	return row.Locked != nil && *row.Locked, nil
}

// PruneWebhookEvents deletes events older than the cutoff; 0 disables.
func (r JobsRepo) PruneWebhookEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := r.q.PruneWebhookEventsBefore(ctx, db.PruneWebhookEventsBeforeParams{Cutoff: cutoff})
	if err != nil {
		return 0, fmt.Errorf("postgres: prune webhook events: %w", err)
	}

	return n, nil
}

// PruneExpiredTokens deletes tokens whose expiry passed the cutoff.
func (r JobsRepo) PruneExpiredTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := r.q.PruneTokensExpiredBefore(ctx, &cutoff)
	if err != nil {
		return 0, fmt.Errorf("postgres: prune tokens: %w", err)
	}

	return n, nil
}
