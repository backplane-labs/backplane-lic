package request

import (
	"strconv"
	"sync"
	"time"
)

// ReplayGuard rejects nonces that have already been seen.
type ReplayGuard interface {
	// CheckAndRemember reports whether nonce is new for fingerprint
	// and atomically records it when it is. Implementations must
	// remember a nonce at least as long as a request stays
	// acceptable, i.e. for the authenticator's MaxAge plus its
	// future skew.
	CheckAndRemember(fingerprint, nonce string) (ok bool)
}

// InMemoryGuard is a ReplayGuard backed by a bounded in-memory map.
// Nonces expire after ttl. Every entry lives exactly ttl, so insertion
// order is expiry order: a FIFO queue lets each call drop the expired
// head in amortized O(1), with no background goroutine. When the map is
// full of unexpired entries, CheckAndRemember fails closed and refuses
// the nonce.
type InMemoryGuard struct {
	mu         sync.Mutex
	entries    map[string]struct{}
	queue      []guardEntry
	head       int
	ttl        time.Duration
	maxEntries int
}

// guardEntry is one queued nonce with its expiry.
type guardEntry struct {
	key string
	exp time.Time
}

// NewInMemoryReplayGuard returns an InMemoryGuard. Entries expire
// after ttl; maxSize limits the total number of entries. Both must be
// positive (ErrInvalidOptions otherwise).
func NewInMemoryReplayGuard(ttl time.Duration, maxSize int) (*InMemoryGuard, error) {
	if ttl <= 0 || maxSize <= 0 {
		return nil, ErrInvalidOptions
	}

	return &InMemoryGuard{
		entries:    make(map[string]struct{}),
		ttl:        ttl,
		maxEntries: maxSize,
	}, nil
}

// CheckAndRemember implements ReplayGuard.
func (g *InMemoryGuard) CheckAndRemember(fingerprint, nonce string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.expire(now)

	key := nonceKey(fingerprint, nonce)
	if _, seen := g.entries[key]; seen {
		return false
	}

	// Full of unexpired entries: fail closed rather than accept a nonce
	// that cannot be remembered.
	if len(g.entries) >= g.maxEntries {
		return false
	}

	g.entries[key] = struct{}{}
	g.queue = append(g.queue, guardEntry{key: key, exp: now.Add(g.ttl)})

	return true
}

// expire drops the expired head of the queue. The caller holds g.mu.
func (g *InMemoryGuard) expire(now time.Time) {
	for g.head < len(g.queue) && !now.Before(g.queue[g.head].exp) {
		delete(g.entries, g.queue[g.head].key)
		g.head++
	}

	// Compact once the dead prefix outweighs the live part: the copy is
	// paid for by the drops, so the cost stays amortized O(1) and the
	// backing array stays bounded.
	if g.head > 0 && g.head >= len(g.queue)-g.head {
		live := copy(g.queue, g.queue[g.head:])
		clear(g.queue[live:])
		g.queue = g.queue[:live]
		g.head = 0
	}
}

// nonceKey builds an injective map key: the length prefix keeps a
// (fingerprint, nonce) pair from colliding with another split of the
// same concatenation.
func nonceKey(fingerprint, nonce string) string {
	return strconv.Itoa(len(fingerprint)) + "|" + fingerprint + nonce
}
