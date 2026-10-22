package httpapi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
)

// maxBuckets caps the in-memory map before the GC sweep runs.
const maxBuckets = 1024

// Throttler counts attempts per key inside a sliding window, mirroring
// the reference's Rack::Attack counters (the password limiter: 5 per
// minute per account segment, IP and email hash).
type Throttler interface {
	// Inc increments the key's counter and answers the new count (an
	// error fails open: the request is allowed through).
	Inc(ctx context.Context, key string, window time.Duration) (int64, error)
}

// MemoryThrottler is the in-process fallback counter: fixed windows in
// a map, garbage-collected on access.

type MemoryThrottler struct {
	mu      sync.Mutex
	buckets map[string]bucket
}

type bucket struct {
	expires time.Time
	count   int64
}

// NewMemoryThrottler builds the in-memory counter.
func NewMemoryThrottler() *MemoryThrottler {
	return &MemoryThrottler{buckets: map[string]bucket{}}
}

// Inc increments the key's window counter.
func (t *MemoryThrottler) Inc(_ context.Context, key string, window time.Duration) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	if len(t.buckets) > maxBuckets {
		for k, b := range t.buckets {
			if b.expires.Before(now) {
				delete(t.buckets, k)
			}
		}
	}

	b, ok := t.buckets[key]
	if !ok || b.expires.Before(now) {
		b = bucket{expires: now.Add(window)}
	}

	b.count++
	t.buckets[key] = b

	return b.count, nil
}

// ValkeyThrottler counts through the cache's raw client (INCR with a
// window expiry), sharing the counter across replicas.
type ValkeyThrottler struct {
	client valkey.Client
}

// NewValkeyThrottler builds the shared counter.
func NewValkeyThrottler(client valkey.Client) *ValkeyThrottler {
	return &ValkeyThrottler{client: client}
}

// Inc increments the key's window counter.
func (t *ValkeyThrottler) Inc(ctx context.Context, key string, window time.Duration) (int64, error) {
	resp := t.client.Do(ctx, t.client.B().Incr().Key(key).Build())

	count, err := resp.ToInt64()
	if err != nil {
		return 0, fmt.Errorf("httpapi: throttle incr: %w", err)
	}

	if count == 1 {
		expire := t.client.Do(ctx,
			t.client.B().Expire().Key(key).Seconds(int64(window.Seconds())).Build())
		if err := expire.Error(); err != nil {
			return count, fmt.Errorf("httpapi: throttle expire: %w", err)
		}
	}

	return count, nil
}
