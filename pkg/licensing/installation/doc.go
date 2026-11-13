// Package installation manages license-bound installation keys and
// machine activation. An installation generates an Ed25519 key pair;
// the machine fingerprint is the lowercase hex SHA-256 of the raw
// 32-byte public key. Activation attaches the fingerprint and the
// base64-encoded public key to the machine's metadata, binding the
// key to the license.
//
// Key creation and storage:
//
//	key, err := installation.NewKey()
//	seed := key.Seed()
//	// ... persist seed ...
//	key, err = installation.ParseKey(seed)
//
// Activation:
//
//	machine, err := installation.Activate(ctx, license, key,
//	    licensing.WithMachineName("my-installation"))
//
// Air-gapped activation:
//
//	req := installation.ActivationRequest{
//	    Fingerprint: key.Fingerprint(),
//	    PublicKey:   base64.StdEncoding.EncodeToString(key.Public()),
//	    LicenseID:   license.ID,
//	    Version:     "1.0.0",
//	}
//	data, _ := json.Marshal(req)
//	// ... transfer data to the vendor side ...
package installation
