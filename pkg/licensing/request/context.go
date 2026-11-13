package request

import (
	"context"
	"crypto/ed25519"
)

// Installation is a machine resolved from a verified request signature.
type Installation struct {
	// Fingerprint is the installation fingerprint that signed the
	// request (the keyid signature parameter).
	Fingerprint string

	// MachineID is the machine UUID.
	MachineID string

	// LicenseID is the license UUID the machine belongs to.
	LicenseID string

	// PublicKey is the machine's raw 32-byte Ed25519 public key,
	// verified to hash to Fingerprint.
	PublicKey ed25519.PublicKey

	// LicenseCode is the licensing API's validation code for the
	// machine's license ("VALID" for an authenticated request).
	LicenseCode string
}

// installationCtxKey is the context key under which Middleware stores
// the authenticated Installation.
type installationCtxKey struct{}

// withInstallation stores inst in ctx.
func withInstallation(ctx context.Context, inst Installation) context.Context {
	return context.WithValue(ctx, installationCtxKey{}, inst)
}

// FromContext returns the Installation stored by Middleware; ok is
// false for a request that was not authenticated.
func FromContext(ctx context.Context) (Installation, bool) {
	inst, ok := ctx.Value(installationCtxKey{}).(Installation)

	return inst, ok
}
