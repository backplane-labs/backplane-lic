package licensing

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Credential and transport errors.
var (
	// ErrTokenInvalid reports an unknown or malformed API token (401).
	ErrTokenInvalid = errors.New("token is invalid")

	// ErrTokenExpired reports an expired API token (401).
	ErrTokenExpired = errors.New("token is expired")

	// ErrTokenNotAllowed reports token authentication the policy forbids (403).
	ErrTokenNotAllowed = errors.New("token authentication is not allowed by policy")

	// ErrLicenseNotAllowed reports license-key authentication the policy
	// forbids (403).
	ErrLicenseNotAllowed = errors.New("license authentication is not allowed by policy")

	// ErrLicenseBanned reports a banned license (403).
	ErrLicenseBanned = errors.New("license is banned")

	// ErrLicenseSuspended reports a suspended license (403).
	ErrLicenseSuspended = errors.New("license is suspended")

	// ErrLicenseExpired reports an expired license that lost access (403).
	ErrLicenseExpiredAuth = errors.New("license is expired")

	// ErrLicenseInvalidKey reports an unknown license key (401).
	ErrLicenseInvalidKey = errors.New("license key is invalid")

	// ErrNotFound reports a 404 for the addressed resource.
	ErrNotFound = errors.New("resource was not found")

	// ErrForbidden reports a 403 the more specific sentinels do not cover.
	ErrForbidden = errors.New("not authorized to perform the request")
)

// Validation outcomes: Validate returns one of these (wrapped in
// APIError, which carries the full detail) when the validation code is
// not VALID. The License returned alongside still holds the complete
// Validation result.
var (
	// ErrLicenseKeyMissing reports a checkout on a license whose key is
	// not known to the client.
	ErrLicenseKeyMissing = errors.New("license key is missing")

	// ErrNotFound reports a validation with code NOT_FOUND: the license
	// does not exist.
	ErrValidationNotFound = errors.New("license does not exist")

	// ErrBanned reports code BANNED: the license owner is banned.
	ErrBanned = errors.New("license owner is banned")

	// ErrSuspended reports code SUSPENDED.
	ErrSuspended = errors.New("license is suspended")

	// ErrExpired reports code EXPIRED.
	ErrExpired = errors.New("license is expired")

	// ErrOverdue reports code OVERDUE: a required check-in is overdue.
	ErrOverdue = errors.New("license is overdue for check in")

	// ErrNotActivated reports codes NO_MACHINE / NO_MACHINES: the license
	// has no activated machine.
	ErrNotActivated = errors.New("license is not activated")

	// ErrTooManyMachines reports code TOO_MANY_MACHINES.
	ErrTooManyMachines = errors.New("license has too many machines")

	// ErrTooManyCores reports code TOO_MANY_CORES.
	ErrTooManyCores = errors.New("license has too many cores")

	// ErrTooManyProcesses reports code TOO_MANY_PROCESSES.
	ErrTooManyProcesses = errors.New("license has too many processes")

	// ErrFingerprintScopeMismatch reports code FINGERPRINT_SCOPE_MISMATCH.
	ErrFingerprintScopeMismatch = errors.New("fingerprint does not match an activated machine")

	// ErrFingerprintScopeRequired reports codes
	// FINGERPRINT_SCOPE_REQUIRED / _EMPTY.
	ErrFingerprintScopeRequired = errors.New("fingerprint scope is required")

	// ErrComponentsScopeMismatch reports code COMPONENTS_SCOPE_MISMATCH.
	ErrComponentsScopeMismatch = errors.New("components do not match activated components")

	// ErrComponentsScopeRequired reports codes
	// COMPONENTS_SCOPE_REQUIRED / _EMPTY.
	ErrComponentsScopeRequired = errors.New("components scope is required")

	// ErrHeartbeatNotStarted reports code HEARTBEAT_NOT_STARTED.
	ErrHeartbeatNotStarted = errors.New("machine heartbeat is required but has not started")

	// ErrHeartbeatDead reports code HEARTBEAT_DEAD, and the API errors
	// MACHINE_HEARTBEAT_DEAD / PROCESS_HEARTBEAT_DEAD on ping.
	ErrHeartbeatDead = errors.New("machine heartbeat is dead")

	// ErrProductScopeMismatch reports code PRODUCT_SCOPE_MISMATCH.
	ErrProductScopeMismatch = errors.New("product scope does not match")

	// ErrPolicyScopeMismatch reports code POLICY_SCOPE_MISMATCH.
	ErrPolicyScopeMismatch = errors.New("policy scope does not match")

	// ErrMachineScopeMismatch reports code MACHINE_SCOPE_MISMATCH.
	ErrMachineScopeMismatch = errors.New("machine scope does not match")

	// ErrUserScopeMismatch reports code USER_SCOPE_MISMATCH.
	ErrUserScopeMismatch = errors.New("user scope does not match")

	// ErrEntitlementsMissing reports codes ENTITLEMENTS_MISSING / _EMPTY.
	ErrEntitlementsMissing = errors.New("license is missing one or more required entitlements")

	// ErrLicenseInvalid reports any validation code without its own
	// sentinel.
	ErrLicenseInvalid = errors.New("license is invalid")
)

// Activation and process errors.
var (
	// ErrMachineAlreadyActivated reports FINGERPRINT_TAKEN.
	ErrMachineAlreadyActivated = errors.New("machine is already activated")

	// ErrMachineLimitExceeded reports MACHINE_LIMIT_EXCEEDED.
	ErrMachineLimitExceeded = errors.New("machine limit has been exceeded")

	// ErrProcessLimitExceeded reports MACHINE_PROCESS_LIMIT_EXCEEDED.
	ErrProcessLimitExceeded = errors.New("process limit has been exceeded")
)

// Cryptographic errors.
var (
	// ErrPublicKeyMissing reports verification attempted without a key.
	ErrPublicKeyMissing = errors.New("public key is missing")

	// ErrPublicKeyInvalid reports an unparsable public key.
	ErrPublicKeyInvalid = errors.New("public key is invalid")

	// ErrSchemeNotSupported reports a key/file scheme the client cannot
	// verify.
	ErrSchemeNotSupported = errors.New("cryptographic scheme is not supported")

	// ErrSignatureMissing reports an absent signature header.
	ErrSignatureMissing = errors.New("signature is missing")

	// ErrSignatureInvalid reports a failed signature verification.
	ErrSignatureInvalid = errors.New("signature is invalid")

	// ErrDigestMissing reports an absent digest header.
	ErrDigestMissing = errors.New("digest is missing")

	// ErrDigestInvalid reports a body digest mismatch.
	ErrDigestInvalid = errors.New("digest is invalid")

	// ErrDateMissing reports an absent Date header.
	ErrDateMissing = errors.New("date is missing")

	// ErrDateInvalid reports an unparsable Date header.
	ErrDateInvalid = errors.New("date is invalid")

	// ErrDateTooOld reports a Date header beyond the clock skew.
	ErrDateTooOld = errors.New("date is too old")

	// ErrClockOutOfSync reports a file issued in the future beyond the
	// clock skew, i.e. a system clock problem or a tampered file.
	ErrClockOutOfSync = errors.New("system clock is out of sync")

	// ErrLicenseKeyNotGenuine reports a license key whose signature does
	// not verify.
	ErrLicenseKeyNotGenuine = errors.New("license key is not genuine")

	// ErrLicenseFileNotGenuine reports a license file whose signature
	// does not verify.
	ErrLicenseFileNotGenuine = errors.New("license file is not genuine")

	// ErrMachineFileNotGenuine reports a machine file whose signature
	// does not verify.
	ErrMachineFileNotGenuine = errors.New("machine file is not genuine")

	// ErrFileNotEncrypted reports Decode on an encrypted file.
	ErrFileNotEncrypted = errors.New("file is encrypted")

	// ErrFileEncrypted reports Decrypt on an unencrypted file.
	ErrFileEncrypted = errors.New("file is not encrypted")

	// ErrFileNotSupported reports a certificate algorithm the client
	// cannot handle.
	ErrFileNotSupported = errors.New("certificate algorithm is not supported")

	// ErrLicenseFileExpired reports an expired license file dataset.
	ErrLicenseFileExpired = errors.New("license file is expired")

	// ErrMachineFileExpired reports an expired machine file dataset.
	ErrMachineFileExpired = errors.New("machine file is expired")

	// ErrCertificateInvalid reports a malformed certificate block.
	ErrCertificateInvalid = errors.New("certificate is invalid")

	// ErrUnbound reports an API operation on a value decoded from an
	// offline file dataset, which has no client attached.
	ErrUnbound = errors.New("value is not bound to a client")
)

// APIError is a structured Keygen error document. All non-2xx responses
// (except 429, see RateLimitError) come back as *APIError, usually
// wrapping one of the sentinel errors above so errors.Is works.
type APIError struct {
	// Status is the HTTP status code.
	Status int

	// Title is the error title, e.g. "Not found".
	Title string

	// Detail is the human-readable explanation.
	Detail string

	// Code is the Keygen error code, e.g. "NOT_FOUND".
	Code string

	// Source points at the offending request part, e.g. pointer
	// "/data/attributes/fingerprint" or parameter "limit".
	Source string

	// RequestID is the request's UUID from X-Request-Id.
	RequestID string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("licensing: %d %s: %s", e.Status, e.Code, e.Detail)
	}

	return fmt.Sprintf("licensing: %d %s", e.Status, e.Code)
}

// Unwrap returns the sentinel for well-known codes so errors.Is matches.
func (e *APIError) Unwrap() error {
	switch e.Status {
	case http.StatusTooManyRequests:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return authSentinel(e.Code)
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return apiSentinel(e.Code)
	}
}

// RateLimitError is a 429 response. It carries the throttling state from
// the X-RateLimit-* and Retry-After headers.
type RateLimitError struct {
	// Window is the active throttle window, e.g. "1m".
	Window string

	// Count is the requests made in the window.
	Count int

	// Limit is the requests allowed in the window.
	Limit int

	// Remaining is the requests left in the window.
	Remaining int

	// Reset is when the window ends.
	Reset time.Time

	// RetryAfter is the seconds until the window ends.
	RetryAfter int
}

func (e *RateLimitError) Error() string {
	return "licensing: rate limit has been exceeded"
}

// authSentinel maps authentication/authorization error codes to sentinels.
func authSentinel(code string) error {
	switch code {
	case "TOKEN_INVALID", "TOKEN_FORMAT_INVALID":
		return ErrTokenInvalid
	case "TOKEN_EXPIRED":
		return ErrTokenExpired
	case "TOKEN_NOT_ALLOWED":
		return ErrTokenNotAllowed
	case "LICENSE_NOT_ALLOWED":
		return ErrLicenseNotAllowed
	case "LICENSE_BANNED":
		return ErrLicenseBanned
	case "LICENSE_SUSPENDED":
		return ErrLicenseSuspended
	case "LICENSE_EXPIRED":
		return ErrLicenseExpiredAuth
	case "LICENSE_INVALID":
		return ErrLicenseInvalidKey
	default:
		return ErrForbidden
	}
}

// apiSentinel maps the remaining well-known error codes to sentinels.
func apiSentinel(code string) error {
	switch code {
	case "FINGERPRINT_TAKEN":
		return ErrMachineAlreadyActivated
	case "MACHINE_LIMIT_EXCEEDED":
		return ErrMachineLimitExceeded
	case "MACHINE_PROCESS_LIMIT_EXCEEDED":
		return ErrProcessLimitExceeded
	case "MACHINE_HEARTBEAT_DEAD", "PROCESS_HEARTBEAT_DEAD":
		return ErrHeartbeatDead
	case "NOT_FOUND":
		return ErrNotFound
	default:
		return nil
	}
}

// validationSentinel maps a validation code to its sentinel error.
func validationSentinel(code ValidationCode) error {
	if code == CodeValid {
		return nil
	}

	if sentinel := scopeSentinel(code); sentinel != nil {
		return sentinel
	}

	return stateSentinel(code)
}

// scopeSentinel maps the scope-related codes to their sentinels.
func scopeSentinel(code ValidationCode) error {
	switch code {
	case CodeFingerprintScopeMismatch:
		return ErrFingerprintScopeMismatch
	case CodeFingerprintScopeRequired, CodeFingerprintScopeEmpty:
		return ErrFingerprintScopeRequired
	case CodeComponentsScopeMismatch:
		return ErrComponentsScopeMismatch
	case CodeComponentsScopeRequired, CodeComponentsScopeEmpty:
		return ErrComponentsScopeRequired
	case CodeProductScopeMismatch:
		return ErrProductScopeMismatch
	case CodePolicyScopeMismatch:
		return ErrPolicyScopeMismatch
	case CodeMachineScopeMismatch:
		return ErrMachineScopeMismatch
	case CodeUserScopeMismatch:
		return ErrUserScopeMismatch
	default:
		return nil
	}
}

// stateSentinel maps the license-state codes to their sentinels.
func stateSentinel(code ValidationCode) error {
	switch code {
	case CodeNotFound:
		return ErrValidationNotFound
	case CodeBanned:
		return ErrBanned
	case CodeSuspended:
		return ErrSuspended
	case CodeExpired:
		return ErrExpired
	case CodeOverdue:
		return ErrOverdue
	case CodeNoMachine, CodeNoMachines:
		return ErrNotActivated
	case CodeTooManyMachines:
		return ErrTooManyMachines
	case CodeTooManyCores:
		return ErrTooManyCores
	case CodeTooManyProcesses:
		return ErrTooManyProcesses
	case CodeHeartbeatNotStarted:
		return ErrHeartbeatNotStarted
	case CodeHeartbeatDead:
		return ErrHeartbeatDead
	case CodeEntitlementsMissing, CodeEntitlementsEmpty:
		return ErrEntitlementsMissing
	default:
		return ErrLicenseInvalid
	}
}

// parseRateLimit builds a RateLimitError from a 429 response's headers.
func parseRateLimit(header http.Header) *RateLimitError {
	err := &RateLimitError{
		Window:     header.Get("X-Ratelimit-Window"),
		RetryAfter: atoiOr(header.Get("Retry-After")),
	}

	err.Count = atoiOr(header.Get("X-Ratelimit-Count"))
	err.Limit = atoiOr(header.Get("X-Ratelimit-Limit"))
	err.Remaining = atoiOr(header.Get("X-Ratelimit-Remaining"))

	if reset := atoi64Or(header.Get("X-Ratelimit-Reset")); reset > 0 {
		err.Reset = time.Unix(reset, 0)
	}

	return err
}

func atoiOr(value string) int {
	n, _ := strconv.Atoi(value)

	return n
}

func atoi64Or(value string) int64 {
	n, _ := strconv.ParseInt(value, 10, 64)

	return n
}
