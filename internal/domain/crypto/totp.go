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
	totpStep      = 30 * time.Second
	totpDigits    = 6
	hotpModulus   = 1_000_000
	hotpMaskLow   = 0x0f
	hotpMaskFirst = 0x7f
)

// VerifyTOTP reports whether code is a valid TOTP for secret at now,
// accepting one step of drift in either direction and blocking empty
// codes.
func VerifyTOTP(secretBase32, code string, now time.Time) bool {
	if code == "" || len(code) != totpDigits {
		return false
	}

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32)
	if err != nil {
		return false
	}

	if _, err := strconv.Atoi(code); err != nil {
		return false
	}

	unix := now.Unix()
	if unix < 0 {
		return false
	}

	counter := unix / int64(totpStep.Seconds())

	for _, drift := range []int64{-1, 0, 1} {
		step := counter + drift
		if step < 0 {
			continue
		}

		if hotp(secret, uint64(step)) == code {
			return true
		}
	}

	return false
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
