// Package jobs is the periodic-jobs runner: every tick takes a PG
// advisory xact lock so exactly one replica runs it, then runs the
// jobs that are due — the heartbeat cull and the prunes Keygen runs on
// cron (sidekiq-cron).
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

// Culler runs the heartbeat cull over the dead machines and processes.
type Culler interface {
	Cull(ctx context.Context, now time.Time) error
}

// CullPeriod is the cull cadence (the reference's cron, every 2 minutes).
const CullPeriod = 2 * time.Minute

// Runner runs the periodic jobs. It holds no state but each job's last
// run, so it is safe for a single goroutine (the composition root's).
type Runner struct {
	store  Store
	culler Culler
	clock  func() time.Time
	last   map[string]time.Time
}

// NewRunner builds the runner.
func NewRunner(store Store, culler Culler, clock func() time.Time) *Runner {
	return &Runner{store: store, culler: culler, clock: clock, last: map[string]time.Time{}}
}

// Tick runs one pass: the advisory-lock transaction (another replica's
// tick makes it a no-op) and, inside it, every due job.
func (r *Runner) Tick(ctx context.Context) error {
	return r.store.Do(ctx, func(ctx context.Context) error {
		locked, err := r.store.TryAdvisoryLock(ctx)
		if err != nil {
			return err
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

// jobs lists the runner's jobs.
func (r *Runner) jobs() []job {
	return []job{
		{name: "heartbeat-cull", period: CullPeriod, run: r.culler.Cull},
		{
			name:   "webhook-event-prune",
			period: Day,
			run: func(ctx context.Context, now time.Time) error {
				_, err := r.store.PruneWebhookEvents(ctx, now.AddDate(0, 0, -WebhookBacklogDays))

				return err
			},
		},
		{
			name:   "token-prune",
			period: Day,
			run: func(ctx context.Context, now time.Time) error {
				_, err := r.store.PruneExpiredTokens(ctx, now.AddDate(0, 0, -ExpiredTokenGraceDays))

				return err
			},
		},
	}
}
