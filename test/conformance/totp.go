package conformance

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP is SHA-1 by RFC 6238
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// The RFC 6238 parameters (30-second step, SHA-1, 6 digits) and the
// RFC 4226 dynamic truncation masks.
const (
	totpStepSeconds = 30
	hotpModulus     = 1_000_000
	hotpMaskOffset  = 0x0f
	hotpMaskFirst   = 0x7f
)

// TOTP computes the current 6-digit code for a base32 secret, so
// scenarios can drive the reference's second-factor flows.
func TOTP(tb testing.TB, secretBase32 string) string {
	tb.Helper()

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32)
	if err != nil {
		tb.Fatalf("conformance: totp secret: %v", err)
	}

	counter := time.Now().Unix() / totpStepSeconds
	if counter < 0 {
		tb.Fatalf("conformance: totp: negative time")
	}

	utc := uint64(counter) //nolint:gosec // guarded above

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], utc)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])

	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & hotpMaskOffset
	value := (uint32(sum[offset])&hotpMaskFirst)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	return fmt.Sprintf("%06d", int(value)%hotpModulus)
}

// OTPVar returns a Prepare hook computing the current TOTP code for the
// secret captured under secretVar into otpVar.
func OTPVar(secretVar, otpVar string) func(testing.TB, map[string]string) {
	return func(tb testing.TB, vars map[string]string) {
		tb.Helper()

		vars[otpVar] = TOTP(tb, vars[secretVar])
	}
}

// OTPFresh is OTPVar for a code the reference has not seen: it waits for
// the next 30-second step (the reference blocks OTP replays via
// last_verified_at, and an earlier step of the same scenario may have
// consumed the current code).
func OTPFresh(secretVar, otpVar string) func(testing.TB, map[string]string) {
	return func(tb testing.TB, vars map[string]string) {
		tb.Helper()

		const step = 30 * time.Second

		next := time.Now().Truncate(step).Add(step + 250*time.Millisecond)

		timer := time.NewTimer(time.Until(next))
		defer timer.Stop()

		<-timer.C

		vars[otpVar] = TOTP(tb, vars[secretVar])
	}
}
