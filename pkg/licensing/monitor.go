package licensing

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"
)

// Heartbeat monitor tuning.
const (
	// heartbeatSafetyMargin is subtracted from the heartbeat window
	// before waiting, to absorb network and scheduling lag.
	heartbeatSafetyMargin = 30 * time.Second

	// heartbeatPingAttempts bounds the retries of a failing ping.
	heartbeatPingAttempts = 3

	// heartbeatPingBackoff spaces the ping retries.
	heartbeatPingBackoff = 2 * time.Second

	// heartbeatJitterFraction bounds the random wait adjustment, so a
	// fleet of clients does not ping in lockstep.
	heartbeatJitterFraction = 10

	// heartbeatMarginDivisor scales the safety margin down for short
	// heartbeat windows.
	heartbeatMarginDivisor = 3

	// jitterBand is the width of the random wait band, in spreads.
	jitterBand = 2
)

// monitorHeartbeat pings at the heartbeat cadence until ctx is done.
// The first ping happens immediately; every ping gets a few retries with
// backoff. It returns nil when ctx is canceled, and the last ping error
// when the heartbeats keep failing.
func monitorHeartbeat(
	ctx context.Context,
	interval time.Duration,
	log *slog.Logger,
	ping func(context.Context) error,
) error {
	if err := pingWithRetry(ctx, log, ping); err != nil {
		return err
	}

	wait := heartbeatWait(interval)

	for {
		timer := time.NewTimer(jitter(wait))

		select {
		case <-ctx.Done():
			timer.Stop()

			return nil
		case <-timer.C:
		}

		if err := pingWithRetry(ctx, log, ping); err != nil {
			if ctx.Err() != nil {
				return nil //nolint:nilerr // a canceled monitor stops cleanly
			}

			return err
		}
	}
}

// heartbeatWait is the pause between pings: the window minus the safety
// margin, floored so tiny windows still make progress.
func heartbeatWait(interval time.Duration) time.Duration {
	margin := heartbeatSafetyMargin
	if interval <= heartbeatMarginDivisor*margin {
		margin = interval / heartbeatMarginDivisor
	}

	return max(interval-margin, time.Second)
}

// jitter randomizes a wait by up to ±1/10.
func jitter(wait time.Duration) time.Duration {
	spread := wait / heartbeatJitterFraction
	if spread <= 0 {
		return wait
	}

	return wait + time.Duration(rand.Int64N(int64(jitterBand*spread))) - spread //nolint:gosec // jitter needs no CSPRNG
}

// pingWithRetry pings with a bounded number of attempts, stopping early
// when ctx is done.
func pingWithRetry(ctx context.Context, log *slog.Logger, ping func(context.Context) error) error {
	var err error

	for attempt := 1; attempt <= heartbeatPingAttempts; attempt++ {
		if err = ping(ctx); err == nil {
			return nil
		}

		log.Debug("heartbeat ping failed", "attempt", attempt, "err", err.Error())

		if ctx.Err() != nil {
			return nil //nolint:nilerr // a canceled monitor stops cleanly
		}

		if attempt < heartbeatPingAttempts {
			timer := time.NewTimer(heartbeatPingBackoff)

			select {
			case <-ctx.Done():
				timer.Stop()

				return nil
			case <-timer.C:
			}
		}
	}

	return err
}
