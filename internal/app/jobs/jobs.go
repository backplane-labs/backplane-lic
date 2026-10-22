// Package jobs is the periodic-jobs runner: every tick takes a PG
// advisory xact lock so exactly one replica runs it, then runs the
// jobs that are due. Keygen runs these on cron (sidekiq-cron); later
// phases add the heartbeat culling and expiration notifications to
// the same runner.
package jobs

import (
	"context"
	"time"
)

// Store is the runner's storage port (satisfied by *postgres.Store).
type Store interface {
	// Do runs fn in one transaction; the advisory lock lives in it.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	TryAdvisoryLock(ctx context.Context) (bool, error)
	PruneWebhookEvents(ctx context.Context, cutoff time.Time) (int64, error)
	PruneExpiredTokens(ctx context.Context, cutoff time.Time) (int64, error)
}

// The retention windows, per the reference's cron digests.
const (
	// WebhookBacklogDays: events older than this are pruned (Keygen's
	// KEYGEN_PRUNE_WEBHOOK_BACKLOG_DAYS default 30).
	WebhookBacklogDays = 30
	// ExpiredTokenGraceDays: tokens stay 90 days past their expiry.
	ExpiredTokenGraceDays = 90
	// Day is the prune cadence (Keygen runs the prunes daily).
	Day = 24 * time.Hour
)

// Runner runs the periodic jobs. It holds no state but each job's last
// run, so it is safe for a single goroutine (the composition root's).
type Runner struct {
	store Store
	clock func() time.Time
	last  map[string]time.Time
}

// NewRunner builds the runner.
func NewRunner(store Store, clock func() time.Time) *Runner {
	return &Runner{store: store, clock: clock, last: map[string]time.Time{}}
}

// Tick runs one pass: the advisory-lock transaction (another replica's
// tick makes it a no-op) and, inside it, every due job.
func (r *Runner) Tick(ctx context.Context) error {
	return r.store.Do(ctx, func(ctx context.Context) error { //nolint:wrapcheck // fn's own error
		locked, err := r.store.TryAdvisoryLock(ctx)
		if err != nil {
			return err //nolint:wrapcheck // the store's own error
		}

		if !locked {
			return nil
		}

		now := r.clock()

		for _, job := range r.jobs() {
			if last, ok := r.last[job.name]; ok && now.Sub(last) < job.period {
				continue
			}

			if err := job.run(ctx, now); err != nil {
				return err
			}

			r.last[job.name] = now
		}

		return nil
	})
}

// job is one periodic unit of work.
type job struct {
	name   string
	period time.Duration
	run    func(ctx context.Context, now time.Time) error
}

// jobs lists the runner's jobs; later phases append the heartbeat
// culling and the expiration notifications.
func (r *Runner) jobs() []job {
	return []job{
		{
			name:   "webhook-event-prune",
			period: Day,
			run: func(ctx context.Context, now time.Time) error {
				_, err := r.store.PruneWebhookEvents(ctx, now.AddDate(0, 0, -WebhookBacklogDays))

				return err //nolint:wrapcheck // the store's own error
			},
		},
		{
			name:   "token-prune",
			period: Day,
			run: func(ctx context.Context, now time.Time) error {
				_, err := r.store.PruneExpiredTokens(ctx, now.AddDate(0, 0, -ExpiredTokenGraceDays))

				return err //nolint:wrapcheck // the store's own error
			},
		},
	}
}
