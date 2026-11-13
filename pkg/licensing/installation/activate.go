// Package installation manages license-bound installation keys and
package installation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// PublicKeyOf reads the "publicKey" entry from the machine's metadata,
// verifies it hashes to the machine's fingerprint, and returns the raw
// 32-byte Ed25519 public key. It returns ErrPublicKeyMissing,
// ErrPublicKeyInvalid, or ErrKeyMismatch on failure.
func PublicKeyOf(machine *licensing.Machine) (ed25519.PublicKey, error) {
	encoded, ok := machine.Metadata.String("publicKey")
	if !ok {
		return nil, fmt.Errorf("%w: machine %s", ErrPublicKeyMissing, machine.ID)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: machine %s: %w", ErrPublicKeyInvalid, machine.ID, err)
	}

	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: machine %s: got %d bytes", ErrPublicKeyInvalid, machine.ID, len(raw))
	}

	pub := ed25519.PublicKey(raw)

	if Fingerprint(pub) != machine.Fingerprint {
		return nil, fmt.Errorf("%w: machine %s", ErrKeyMismatch, machine.ID)
	}

	return pub, nil
}

// generatedMetaKeys is the number of metadata entries the key adds:
// fingerprint and publicKey.
const generatedMetaKeys = 2

// activationMetadata builds the metadata map merged with the caller's
// metadata. publicKey and fingerprint from the Key always win.
func activationMetadata(key Key, callerMeta map[string]string) map[string]string {
	merged := make(map[string]string, len(callerMeta)+generatedMetaKeys)

	for k, v := range callerMeta {
		merged[k] = v
	}

	merged["fingerprint"] = key.Fingerprint()
	merged["publicKey"] = base64.StdEncoding.EncodeToString(key.Public())

	return merged
}

// Activate activates a machine for the license with the installation
// key's fingerprint and publicKey metadata. The caller's metadata is
// merged; publicKey and fingerprint from the key always win.
func Activate(
	ctx context.Context,
	license *licensing.License,
	key Key,
	opts ...licensing.ActivationOption,
) (*licensing.Machine, error) {
	callerOpts := licensing.ActivationOptions{Metadata: map[string]string{}}

	for _, o := range opts {
		o(&callerOpts)
	}

	md := activationMetadata(key, callerOpts.Metadata)

	allOpts := []licensing.ActivationOption{
		licensing.WithMachineMetadata(md),
	}

	if callerOpts.Name != "" {
		allOpts = append(allOpts, licensing.WithMachineName(callerOpts.Name))
	}

	if callerOpts.Hostname != "" {
		allOpts = append(allOpts, licensing.WithMachineHostname(callerOpts.Hostname))
	}

	if callerOpts.Platform != "" {
		allOpts = append(allOpts, licensing.WithMachinePlatform(callerOpts.Platform))
	}

	if callerOpts.Cores != 0 {
		allOpts = append(allOpts, licensing.WithMachineCores(callerOpts.Cores))
	}

	if len(callerOpts.Components) > 0 {
		allOpts = append(allOpts, licensing.WithActivationComponents(callerOpts.Components...))
	}

	machine, err := license.Activate(ctx, key.Fingerprint(), allOpts...)
	if err != nil {
		return nil, fmt.Errorf("activate installation: %w", err)
	}

	return machine, nil
}

// ActivationRequest is the payload for air-gapped activation: the
// installation sends this to the vendor side, which activates a machine
// from it on the vendor's network.
type ActivationRequest struct {
	// Fingerprint is the lowercase hex SHA-256 of the public key.
	Fingerprint string `json:"fingerprint"`

	// PublicKey is standard base64 of the 32-byte Ed25519 public key.
	PublicKey string `json:"public_key"`

	// LicenseID is the license UUID (not the license key).
	LicenseID string `json:"license_id"`

	// Version is the build version of the installation.
	Version string `json:"version"`
}

// ParseActivationRequest decodes an activation request from JSON.
func ParseActivationRequest(data []byte) (*ActivationRequest, error) {
	req := &ActivationRequest{}

	if err := json.Unmarshal(data, req); err != nil {
		return nil, fmt.Errorf("parse activation request: %w", err)
	}

	if req.Fingerprint == "" {
		return nil, fmt.Errorf("%w: fingerprint is required", ErrFingerprintRequired)
	}

	if req.LicenseID == "" {
		return nil, fmt.Errorf("%w: license_id is required", ErrLicenseIDRequired)
	}

	// PublicKey may be empty if the vendor validates differently.

	return req, nil
}
