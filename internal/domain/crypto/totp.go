// TOTP verification (RFC 6238, SHA-1, 6 digits, 30-second step, one
// window of drift either way — the reference's drift_behind 10s
// tolerance, rounded to a window).
package crypto

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP is SHA-1 by RFC 6238 (the reference's ROTP default)
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"
)

// totpStep, totpDigits and the HOTP masks match the reference's ROTP
// defaults and RFC 4226's dynamic truncation.
const (
	totpStep        = 30 * time.Second
	totpDigits      = 6
	hotpModulus     = 1_000_000
	hotpMaskLow     = 0x0f
	hotpMaskFirst   = 0x7f
	driftBehindSecs = 10 // the reference's SECOND_FACTOR_DRIFT
)

// driftBehindSeconds aliases the constant for the counter math below.
const driftBehindSeconds = driftBehindSecs

// VerifyTOTP reports whether code is a valid TOTP for secret at now,
// accepting one step of drift in either direction and blocking empty
// codes.
func VerifyTOTP(secretBase32, code string, now time.Time) bool {
	_, ok, _ := VerifyTOTPAfter(secretBase32, code, now, nil)

	return ok
}

// VerifyTOTPAfter is VerifyTOTP with replay protection: a code whose
// timestamp is not strictly after the previously verified one fails.
// It answers the matched step's timestamp (for persisting as the new
// replay guard).
func VerifyTOTPAfter(
	secretBase32, code string,
	now time.Time,
	after *time.Time,
) (time.Time, bool, error) {
	if code == "" || len(code) != totpDigits {
		return time.Time{}, false, nil
	}

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32)
	if err != nil {
		// An undecodable secret simply never verifies (a stored-secret
		// corruption, not the caller's error).
		return time.Time{}, false, nil //nolint:nilerr // undecodable secrets never verify
	}

	if _, err := strconv.Atoi(code); err != nil {
		return time.Time{}, false, nil //nolint:nilerr // a non-numeric code simply fails
	}

	unix := now.Unix()
	if unix < 0 {
		return time.Time{}, false, nil
	}

	counter := unix / int64(totpStep.Seconds())

	// The reference verifies with drift_behind 10s: the current step,
	// plus the previous one while now is within 10s of its end — never a
	// future step.
	counters := []int64{counter, (unix - driftBehindSeconds) / int64(totpStep.Seconds())}

	for _, step := range counters {
		if step < 0 {
			continue
		}

		if hotp(secret, uint64(step)) != code {
			continue
		}

		ts := time.Unix(step*int64(totpStep.Seconds()), 0).UTC()
		if after != nil && !ts.After(*after) {
			// A replayed (or older) code.
			return time.Time{}, false, nil
		}

		return ts, true, nil
	}

	return time.Time{}, false, nil
}

func hotp(secret []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])

	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & hotpMaskLow
	value := (uint32(sum[offset])&hotpMaskFirst)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	return fmt.Sprintf("%06d", int(value)%hotpModulus)
}
