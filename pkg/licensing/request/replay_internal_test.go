package request

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// newGuard builds a replay guard the test expects to be valid.
func newGuard(tb testing.TB, ttl time.Duration, size int) *InMemoryGuard {
	tb.Helper()

	guard, err := NewInMemoryReplayGuard(ttl, size)
	if err != nil {
		tb.Fatal(err)
	}

	return guard
}

func TestReplayGuard_CheckAndRemember(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		calls [][2]string
		want  []bool
	}{
		{
			name: "fresh nonce accepted, replay rejected",
			calls: [][2]string{
				{"fp1", "nonce1"},
				{"fp1", "nonce1"},
				{"fp1", "nonce2"},
			},
			want: []bool{true, false, true},
		},
		{
			name: "same nonce under another fingerprint accepted",
			calls: [][2]string{
				{"fp1", "nonce1"},
				{"fp2", "nonce1"},
			},
			want: []bool{true, true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			guard := newGuard(t, time.Minute, 1000)

			for i, call := range tc.calls {
				if got := guard.CheckAndRemember(call[0], call[1]); got != tc.want[i] {
					t.Errorf("call %d (%s,%s) = %v, want %v", i, call[0], call[1], got, tc.want[i])
				}
			}
		})
	}
}

func TestReplayGuard_ExpiredNonceForgotten(t *testing.T) {
	t.Parallel()

	guard := newGuard(t, 10*time.Millisecond, 1000)

	if !guard.CheckAndRemember("fp", "n1") {
		t.Fatal("first nonce should be accepted")
	}

	time.Sleep(30 * time.Millisecond)

	if !guard.CheckAndRemember("fp", "n1") {
		t.Fatal("expired nonce should be forgotten and accepted again")
	}
}

func TestReplayGuard_FailsClosedWhenFull(t *testing.T) {
	t.Parallel()

	guard := newGuard(t, time.Minute, 2)

	if !guard.CheckAndRemember("fp", "n1") {
		t.Fatal("first nonce should be accepted")
	}

	if !guard.CheckAndRemember("fp", "n2") {
		t.Fatal("second nonce should be accepted")
	}

	// The map is full of unexpired entries: the guard refuses rather
	// than accept a nonce it cannot remember.
	if guard.CheckAndRemember("fp", "n3") {
		t.Fatal("nonce should be refused when the guard is full")
	}
}

func TestReplayGuard_Concurrent(t *testing.T) {
	t.Parallel()

	guard := newGuard(t, time.Minute, 1000)

	const workers, perWorker = 8, 100

	var wg sync.WaitGroup

	accepted := make([]int, workers)

	for w := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range perWorker {
				if guard.CheckAndRemember("fp", nonceOf(w, i)) {
					accepted[w]++
				}
			}
		}()
	}

	wg.Wait()

	total := 0

	for _, n := range accepted {
		total += n
	}

	if total != workers*perWorker {
		t.Errorf("accepted = %d, want %d", total, workers*perWorker)
	}
}

func TestNewReplayGuard_RejectsBadConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ttl  time.Duration
		size int
	}{
		{"zero ttl", 0, 10},
		{"negative ttl", -time.Second, 10},
		{"zero size", time.Minute, 0},
		{"negative size", time.Minute, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewInMemoryReplayGuard(tc.ttl, tc.size); !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("err = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

// nonceOf builds a distinct nonce for worker w and iteration i.
func nonceOf(w, i int) string {
	return strconv.Itoa(w) + "-" + strconv.Itoa(i)
}

func TestReplayGuard_ChurnStaysBounded(t *testing.T) {
	t.Parallel()

	guard := newGuard(t, 5*time.Millisecond, 50)

	for i := range 2000 {
		if !guard.CheckAndRemember("fp", strconv.Itoa(i)) {
			time.Sleep(6 * time.Millisecond)

			if !guard.CheckAndRemember("fp", strconv.Itoa(i)) {
				t.Fatalf("nonce %d refused after expiry made room", i)
			}
		}
	}

	guard.mu.Lock()
	defer guard.mu.Unlock()

	if len(guard.entries) > 50 || len(guard.queue) > 100 {
		t.Errorf("entries = %d, queue = %d: not bounded", len(guard.entries), len(guard.queue))
	}
}

func BenchmarkReplayGuard(b *testing.B) {
	guard := newGuard(b, time.Minute, 1<<20)

	i := 0

	for b.Loop() {
		guard.CheckAndRemember("fp", strconv.Itoa(i))

		i++
	}
}
