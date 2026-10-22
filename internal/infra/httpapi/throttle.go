package httpapi

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
)

// maxBuckets caps the in-memory map before the GC sweep runs.
const maxBuckets = 1024

// Throttler counts attempts per key inside a fixed window, mirroring
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
	clock   func() time.Time
	buckets map[string]bucket
}

type bucket struct {
	expires time.Time
	count   int64
}

// NewMemoryThrottler builds the in-memory counter.
func NewMemoryThrottler() *MemoryThrottler {
	return &MemoryThrottler{buckets: map[string]bucket{}, clock: time.Now}
}

// Inc increments the key's window counter.
func (t *MemoryThrottler) Inc(_ context.Context, key string, window time.Duration) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock()

	if len(t.buckets) > maxBuckets {
		for k, b := range t.buckets {
			if b.expires.Before(now) {
				delete(t.buckets, k)
			}
		}
	}

	b, ok := t.buckets[key]
	if !ok || !now.Before(b.expires) {
		b = bucket{expires: now.Truncate(window).Add(window)}
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
	now := time.Now()
	reset := now.Truncate(window).Add(window)
	// Epoch namespaces reproduce Rack::Attack's fixed-window counters. The
	// increment and expiration are atomic: a lost EXPIRE cannot leak a bucket.
	key = "lic/throttle/" + strconv.FormatInt(now.Unix()/int64(window.Seconds()), 10) + "/" + key
	script := valkey.NewLuaScript(`
local n = redis.call('INCR', KEYS[1])
redis.call('PEXPIREAT', KEYS[1], ARGV[1])
return n
`)

	count, err := script.Exec(ctx, t.client, []string{key}, []string{strconv.FormatInt(reset.UnixMilli(), 10)}).ToInt64()
	if err != nil {
		return 0, fmt.Errorf("httpapi: throttle increment: %w", err)
	}

	return count, nil
}
