package installation

import "errors"

// Activation and key errors matched with errors.Is.
var (
	// ErrKeyMismatch reports that the public key from machine metadata
	// does not hash to the machine's fingerprint.
	ErrKeyMismatch = errors.New("public key does not hash to the machine fingerprint")

	// ErrPublicKeyMissing reports that the machine metadata has no
	// publicKey entry.
	ErrPublicKeyMissing = errors.New("machine metadata does not contain a public key")

	// ErrPublicKeyInvalid reports that the publicKey metadata value is
	// not valid base64 or not a 32-byte key.
	ErrPublicKeyInvalid = errors.New("machine metadata public key is invalid")

	// ErrSeedSize reports that a seed passed to ParseKey has the wrong
	// length.
	ErrSeedSize = errors.New("seed must be 32 bytes")

	// ErrFingerprintRequired reports that an activation request is
	// missing the fingerprint field.
	ErrFingerprintRequired = errors.New("activation request fingerprint is required")

	// ErrLicenseIDRequired reports that an activation request is missing
	// the license_id field.
	ErrLicenseIDRequired = errors.New("activation request license_id is required")
)
