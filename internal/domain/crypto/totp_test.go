package crypto //nolint:testpackage // verify boundary counters with the private RFC HOTP primitive

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestTOTPDriftAndReplay(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	start := time.Unix(1_234_567_800, 0).UTC()
	counter := uint64(start.Unix() / 30)
	previous := hotp(secret, counter-1)
	current := hotp(secret, counter)
	future := hotp(secret, counter+1)

	for _, offset := range []time.Duration{0, 9 * time.Second, 10 * time.Second, 29 * time.Second} {
		now := start.Add(offset)
		if !VerifyTOTP(encoded, current, now) {
			t.Errorf("current code rejected at %s", offset)
		}

		if got := VerifyTOTP(encoded, previous, now); got != (offset < 10*time.Second) {
			t.Errorf("previous code at %s: valid=%t", offset, got)
		}

		if VerifyTOTP(encoded, future, now) {
			t.Errorf("future code accepted at %s", offset)
		}
	}

	matched, ok, err := VerifyTOTPAfter(encoded, current, start, nil)
	if err != nil || !ok || !matched.Equal(start) {
		t.Fatalf("first verification: %s %t %v", matched, ok, err)
	}

	if _, replayed, replayErr := VerifyTOTPAfter(encoded, current, start, &matched); replayErr != nil || replayed {
		t.Fatalf("replay accepted: %t %v", replayed, replayErr)
	}

	for _, code := range []string{"", "12345", "1234567", "+12345", "abcdef"} {
		if VerifyTOTP(encoded, code, start) {
			t.Errorf("malformed code accepted: %q", code)
		}
	}
}
