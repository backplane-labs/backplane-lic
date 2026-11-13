package request

import (
	"errors"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// Authentication outcomes matched with errors.Is. Callers can branch
// on these codes to map to HTTP 401/403/503 responses.
var (
	// ErrInvalidOptions reports a negative AuthOptions bound.
	ErrInvalidOptions = errors.New("request: authenticator options must not be negative")

	// ErrSignatureMissing reports a missing or unparsable Signature or
	// Signature-Input header, or missing signature parameters required
	// by this package (keyid, created, nonce).
	ErrSignatureMissing = errors.New("signature is missing")

	// ErrSignatureInvalid reports a signature that does not verify:
	// a malformed keyid, uncovered required components, a disallowed
	// algorithm, or a failing Ed25519 verification.
	ErrSignatureInvalid = errors.New("signature is invalid")

	// ErrDigestMismatch reports a Content-Digest that does not match the body.
	ErrDigestMismatch = errors.New("content-digest does not match request body")

	// ErrDigestRequired reports a missing Content-Digest on a request with a body.
	ErrDigestRequired = errors.New("content-digest is required for requests with a body")

	// ErrBodyTooLarge reports a request body larger than the configured maximum.
	ErrBodyTooLarge = errors.New("request body exceeds the maximum size")

	// ErrCreatedExpired reports a created timestamp outside the allowed window.
	ErrCreatedExpired = errors.New("signature created timestamp is outside the allowed window")

	// ErrCreatedMissing reports a missing created parameter.
	ErrCreatedMissing = errors.New("signature created parameter is missing")

	// ErrNonceReplayed reports a nonce that was already seen.
	ErrNonceReplayed = errors.New("nonce has already been used")

	// ErrNonceMissing reports a missing nonce parameter.
	ErrNonceMissing = errors.New("signature nonce parameter is missing")

	// ErrUnknownKey reports a fingerprint that does not match any machine.
	ErrUnknownKey = errors.New("signing key is not a known machine fingerprint")

	// ErrKeyMismatch reports a machine whose metadata publicKey does not
	// hash to the fingerprint.
	ErrKeyMismatch = errors.New("machine public key does not match its fingerprint")

	// ErrLicenseInvalid reports that the machine's license is not valid.
	// Refusals that carry a Keygen validation code are *LicenseError,
	// which matches this sentinel.
	ErrLicenseInvalid = errors.New("machine license is not valid")

	// ErrUpstreamUnavailable reports that the licensing API is unreachable.
	ErrUpstreamUnavailable = errors.New("licensing API is unavailable")
)

// LicenseError is a license refusal that carries the Keygen validation
// code. It matches ErrLicenseInvalid via errors.Is and unwraps to the
// licensing client's error (so errors.Is also matches licensing
// sentinels such as licensing.ErrExpired) when one caused the refusal.
type LicenseError struct {
	// Code is the Keygen validation code, e.g. EXPIRED. It is empty
	// when the validation result was missing entirely.
	Code licensing.ValidationCode

	// Detail is the Keygen human-readable explanation.
	Detail string

	// cause is the licensing client error that carried the validation
	// result, if any.
	cause error
}

// Error implements the error interface.
func (e *LicenseError) Error() string {
	if e.Code == "" {
		return ErrLicenseInvalid.Error() + ": " + e.Detail
	}

	return ErrLicenseInvalid.Error() + ": " + string(e.Code) + ": " + e.Detail
}

// Unwrap returns the licensing client error that caused the refusal.
func (e *LicenseError) Unwrap() error {
	return e.cause
}

// Is reports whether target is ErrLicenseInvalid, so errors.Is matches
// the sentinel for every coded refusal.
func (e *LicenseError) Is(target error) bool {
	return target == ErrLicenseInvalid
}
