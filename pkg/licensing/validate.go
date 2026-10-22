package licensing

import (
	"fmt"
	"time"
)

// ValidationCode is a license validation code.
type ValidationCode string

// Validation codes. Codes without a dedicated sentinel error map to
// ErrLicenseInvalid.
const (
	CodeValid                    ValidationCode = "VALID"
	CodeNotFound                 ValidationCode = "NOT_FOUND"
	CodeBanned                   ValidationCode = "BANNED"
	CodeSuspended                ValidationCode = "SUSPENDED"
	CodeExpired                  ValidationCode = "EXPIRED"
	CodeOverdue                  ValidationCode = "OVERDUE"
	CodeNoMachine                ValidationCode = "NO_MACHINE"
	CodeNoMachines               ValidationCode = "NO_MACHINES"
	CodeTooManyMachines          ValidationCode = "TOO_MANY_MACHINES"
	CodeTooManyCores             ValidationCode = "TOO_MANY_CORES"
	CodeTooManyProcesses         ValidationCode = "TOO_MANY_PROCESSES"
	CodeFingerprintScopeMismatch ValidationCode = "FINGERPRINT_SCOPE_MISMATCH"
	CodeFingerprintScopeRequired ValidationCode = "FINGERPRINT_SCOPE_REQUIRED"
	CodeFingerprintScopeEmpty    ValidationCode = "FINGERPRINT_SCOPE_EMPTY"
	CodeComponentsScopeMismatch  ValidationCode = "COMPONENTS_SCOPE_MISMATCH"
	CodeComponentsScopeRequired  ValidationCode = "COMPONENTS_SCOPE_REQUIRED"
	CodeComponentsScopeEmpty     ValidationCode = "COMPONENTS_SCOPE_EMPTY"
	CodeHeartbeatNotStarted      ValidationCode = "HEARTBEAT_NOT_STARTED"
	CodeHeartbeatDead            ValidationCode = "HEARTBEAT_DEAD"
	CodeProductScopeMismatch     ValidationCode = "PRODUCT_SCOPE_MISMATCH"
	CodeProductScopeRequired     ValidationCode = "PRODUCT_SCOPE_REQUIRED"
	CodePolicyScopeMismatch      ValidationCode = "POLICY_SCOPE_MISMATCH"
	CodePolicyScopeRequired      ValidationCode = "POLICY_SCOPE_REQUIRED"
	CodeMachineScopeMismatch     ValidationCode = "MACHINE_SCOPE_MISMATCH"
	CodeMachineScopeRequired     ValidationCode = "MACHINE_SCOPE_REQUIRED"
	CodeUserScopeMismatch        ValidationCode = "USER_SCOPE_MISMATCH"
	CodeUserScopeRequired        ValidationCode = "USER_SCOPE_REQUIRED"
	CodeEntitlementsMissing      ValidationCode = "ENTITLEMENTS_MISSING"
	CodeEntitlementsEmpty        ValidationCode = "ENTITLEMENTS_SCOPE_EMPTY"
)

// ValidationResult is the meta object of a validation response.
type ValidationResult struct {
	// Code is the machine-readable outcome.
	Code ValidationCode `json:"code"`

	// Detail is the human-readable outcome.
	Detail string `json:"detail"`

	// Valid is the server's verdict; it can be true alongside a
	// soft-fail code (e.g. EXPIRED under MAINTAIN_ACCESS).
	Valid bool `json:"valid"`

	// TS is when the server processed the validation.
	TS time.Time `json:"ts"`

	// Scope echoes the validated scope when one was sent.
	Scope *validateScope `json:"scope,omitempty"`
}

// Err returns the sentinel error for the result's code, or nil for
// VALID.
func (r *ValidationResult) Err() error {
	if r == nil || r.Code == CodeValid {
		return nil
	}

	sentinel := validationSentinel(r.Code)

	return fmt.Errorf("%w: %s", sentinel, r.Detail)
}
