package domain

import "time"

// LicenseValidationEvent carries the license and the validation metadata
// included in the reference's webhook document.
type LicenseValidationEvent struct {
	License License
	Valid   bool
	Detail  string
	Code    string
	Time    time.Time
	Nonce   *int64
	Scope   map[string]any
}
