package licensing

import (
	"encoding/json"
	"time"
)

// Scheme is a license key signing scheme.
type Scheme string

// The license key schemes the client can verify.
const (
	// SchemeEd25519 signs keys with the account's ed25519 key.
	SchemeEd25519 Scheme = "ED25519_SIGN"
	// SchemeECDSAP256 signs keys with the account's ECDSA P-256 key.
	SchemeECDSAP256 Scheme = "ECDSA_P256_SIGN"
	// SchemeRSAPKCS1 signs keys with RSA PKCS#1 v1.5 over the raw seed
	// (v1 format, no key/ prefix).
	SchemeRSAPKCS1 Scheme = "RSA_2048_PKCS1_SIGN"
	// SchemeRSAPKCS1V2 signs keys with RSA PKCS#1 v1.5 over key/<seed>.
	SchemeRSAPKCS1V2 Scheme = "RSA_2048_PKCS1_SIGN_V2"
	// SchemeRSAPSS signs keys with RSA-PSS over the raw seed (v1 format).
	SchemeRSAPSS Scheme = "RSA_2048_PKCS1_PSS_SIGN"
	// SchemeRSAPSSV2 signs keys with RSA-PSS over key/<seed>.
	SchemeRSAPSSV2 Scheme = "RSA_2048_PKCS1_PSS_SIGN_V2"
)

// Metadata is a resource's free-form metadata. Values are raw JSON so
// nested structures survive a round trip; use String for the common
// scalar case.
type Metadata map[string]json.RawMessage

// String returns a metadata value as a string, reporting whether the
// value exists and is a string.
func (m Metadata) String(key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}

	return value, true
}

// HeartbeatStatus is a machine's heartbeat lifecycle state.
type HeartbeatStatus string

// Machine heartbeat statuses.
const (
	HeartbeatNotStarted  HeartbeatStatus = "NOT_STARTED"
	HeartbeatAlive       HeartbeatStatus = "ALIVE"
	HeartbeatDead        HeartbeatStatus = "DEAD"
	HeartbeatResurrected HeartbeatStatus = "RESURRECTED"
)

// ProcessStatus is a process's heartbeat lifecycle state.
type ProcessStatus string

// Process statuses.
const (
	ProcessAlive       ProcessStatus = "ALIVE"
	ProcessDead        ProcessStatus = "DEAD"
	ProcessResurrected ProcessStatus = "RESURRECTED"
)

// fileClock bounds the clock skew checks of decoded file datasets.
type fileClock struct {
	skew SkewGuard
}

// checkDataset applies the shared timestamp checks of decoded datasets:
// the issue time must not be in the future beyond the skew, and a file
// with a TTL must not be past its expiry.
func (c fileClock) checkDataset(issued time.Time, expiry *time.Time, ttl int, expired error) error {
	if c.skew.max > 0 && issued.After(time.Now().Add(c.skew.max)) {
		return ErrClockOutOfSync
	}

	if ttl != 0 && expiry != nil && time.Now().After(*expiry) {
		return expired
	}

	return nil
}
