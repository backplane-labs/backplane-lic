package domain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestMetadataLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		metadata map[string]any
		want     []string
	}{
		{"empty", map[string]any{}, nil},
		{"exact bytes", map[string]any{"a": strings.Repeat("x", 16376)}, nil},
		{"over bytes", map[string]any{"a": strings.Repeat("x", 16377)}, []string{"too large (exceeded limit of 16384 bytes)"}},
		{"html bytes", map[string]any{"a": strings.Repeat("<>&", 5000)}, nil},
		{"unicode bytes", map[string]any{"a": strings.Repeat(" ", 5000)}, nil},
		{"literal escape bytes", map[string]any{"a": strings.Repeat(`\u2028`, 2500)}, []string{"too large (exceeded limit of 16384 bytes)"}},
		{"depth four", map[string]any{"a": []any{map[string]any{"b": []any{1}}}}, nil},
		{"depth five", map[string]any{"a": []any{map[string]any{"b": []any{[]any{1}}}}}, []string{"too many items (exceeded limit of 4 items)"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.MetadataViolation(test.metadata); got != firstViolation(test.want) {
				t.Fatalf("violations = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMetadataViolationPriority(t *testing.T) {
	t.Parallel()

	metadata := make(map[string]any)
	for index := range 63 {
		metadata[fmt.Sprintf("k%d", index)] = index
	}

	metadata["nested"] = []any{map[string]any{"key": strings.Repeat("x", 17000)}}

	want := []string{
		"too large (exceeded limit of 16384 bytes)",
		"too many keys (exceeded limit of 64 keys)",
	}
	if got := domain.MetadataViolation(metadata); got != firstViolation(want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

func firstViolation(violations []string) string {
	if len(violations) == 0 {
		return ""
	}

	return violations[0]
}
