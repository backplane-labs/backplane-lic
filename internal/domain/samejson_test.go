package domain_test

import (
	"math"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestSameJSON(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a, b any
		want bool
	}{
		{"equal maps", map[string]any{"tier": "gold", "n": 1}, map[string]any{"n": 1.0, "tier": "gold"}, true},
		{"different value", map[string]any{"tier": "gold"}, map[string]any{"tier": "silver"}, false},
		{"nil and empty list differ", []string(nil), []string{}, false},
		{"unencodable", math.NaN(), math.NaN(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.SameJSON(tc.a, tc.b); got != tc.want {
				t.Fatalf("SameJSON = %v, want %v", got, tc.want)
			}
		})
	}
}

func BenchmarkSameJSON(b *testing.B) {
	a := map[string]any{"tier": "gold", "seats": 10}
	for b.Loop() {
		domain.SameJSON(a, a)
	}
}
