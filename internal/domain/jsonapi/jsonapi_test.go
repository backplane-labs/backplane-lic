package jsonapi_test

import (
	"testing"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
)

// TestUnderscoreKey pins Rails' underscore behavior on the tricky
// shapes (acronyms, digits, separators).
func TestUnderscoreKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"firstName", "first_name"},
		{"fullName", "full_name"},
		{"createdAt", "created_at"},
		{"URL", "url"},
		{"APIVersion", "api_version"},
		{"HTTPSProxy", "https_proxy"},
		{"secondaryKeys", "secondary_keys"},
		{"key-2FA", "key_2_fa"},
		{"key2FA", "key2_fa"},
		{"already_snake", "already_snake"},
	} {
		if got := jsonapi.UnderscoreKey(tc.in); got != tc.want {
			t.Errorf("UnderscoreKey(%q): %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestCamelizeKey pins lowerCamelCase, the wire direction.
func TestCamelizeKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"first_name", "firstName"},
		{"api_version", "apiVersion"},
		{"key_2fa", "key2fa"},
		{"dashed-key", "dashedKey"},
		{"plain", "plain"},
	} {
		if got := jsonapi.CamelizeKey(tc.in); got != tc.want {
			t.Errorf("CamelizeKey(%q): %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestMetadataRoundTrip walks a metadata payload through the wire
// transform and back.
func TestMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	wireIn := map[string]any{
		"orderId": "order-1",
		"nested":  map[string]any{"creationDate": "2026-01-01"},
		"tags":    []any{map[string]any{"tagName": "x"}},
	}

	stored, ok := jsonapi.UnderscoreDeep(wireIn).(map[string]any)
	if !ok {
		t.Fatalf("underscore: %T", jsonapi.UnderscoreDeep(wireIn))
	}

	if _, hasOrder := stored["order_id"]; !hasOrder {
		t.Errorf("underscored: %v", stored)
	}

	back, ok := jsonapi.CamelizeDeep(stored).(map[string]any)
	if !ok {
		t.Fatalf("camelize: %T", jsonapi.CamelizeDeep(stored))
	}

	if _, hasOrder := back["orderId"]; !hasOrder {
		t.Errorf("round trip lost the key: %v", back)
	}

	nested, ok := back["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested: %T", back["nested"])
	}

	if _, hasDate := nested["creationDate"]; !hasDate {
		t.Errorf("round trip lost the nested key: %v", nested)
	}
}

// TestTimestamp pins the exact reference rendering: three fractional
// digits and a Z suffix, UTC.
func TestTimestamp(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("plus3", 3*60*60)
	got := jsonapi.Timestamp(time.Date(2026, 10, 22, 15, 4, 5, 123456789, loc))

	if want := "2026-10-22T12:04:05.123Z"; got != want {
		t.Errorf("timestamp: %q want %q", got, want)
	}
}
