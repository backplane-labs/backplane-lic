// Package licensing is an idiomatic Go client for the Keygen licensing
// API (https://keygen.sh), working both against Keygen CE/EE and any
// 1:1 compatible implementation.
//
// A Client is created from a Config and is safe for concurrent use.
// Authenticate either with a license key (the end-user's credential) or
// an API token:
//
//	client, err := licensing.New(licensing.Config{
//		Account:    "example-com",
//		LicenseKey: licenseKey,
//		PublicKey:  accountEd25519Hex,
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Validations return typed sentinel errors, matched with errors.Is:
//
//	license, err := client.Validate(ctx, licensing.WithFingerprint(fp))
//	switch {
//	case err == nil:
//		// licensed
//	case errors.Is(err, licensing.ErrNotActivated):
//		_, err = license.Activate(ctx, fp)
//	case errors.Is(err, licensing.ErrExpired),
//		errors.Is(err, licensing.ErrSuspended):
//		// refuse to run
//	}
//
// With Config.PublicKey set, every API response is verified against the
// account's signature, and offline artifacts can be verified and
// decrypted without touching the network:
//
//	file, err := license.Checkout(ctx)
//	...
//	if err := file.Verify(verifier); err != nil { ... }
//	dataset, err := file.Decrypt(licenseKey)
//
// Heartbeats run in the caller's goroutine, tied to a context:
//
//	ctx, stop := context.WithCancel(context.Background())
//	go func() { _ = machine.Monitor(ctx) }()
//	defer stop()
//
// Webhooks received from the API can be verified with
// Verifier.VerifyWebhook.
package licensing
