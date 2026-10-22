package webhook //nolint:testpackage // drives the worker's unexported client

import (
	"testing"
	"time"
)

// TestBackoff checks the reference's sidekiq curve: count**4 seconds
// plus a jitter of 10..100 minutes, strictly growing with the count.
func TestBackoff(t *testing.T) {
	t.Parallel()

	for _, count := range []int{1, 2, 3, 5, 10, 15} {
		lower := time.Duration(count*count*count*count)*time.Second + 10*time.Minute
		upper := time.Duration(count*count*count*count)*time.Second + 100*time.Minute

		for _, random := range []float64{0, 0.25, 0.5, 0.75, 1} {
			got := Backoff(count, random)

			if got < lower || got > upper {
				t.Errorf("Backoff(%d, %v) = %v, want within [%v, %v]", count, random, got, lower, upper)
			}
		}
	}

	if Backoff(4, 0) >= Backoff(5, 0) {
		t.Errorf("Backoff(4) = %v not below Backoff(5) = %v", Backoff(4, 0), Backoff(5, 0))
	}
}
