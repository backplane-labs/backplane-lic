// Package request signs and verifies HTTP requests with RFC 9421
// HTTP Message Signatures (Ed25519). The keyid is the installation
// fingerprint; the public key comes from the machine's metadata.
//
// Client side:
//
//	key, _ := installation.NewKey()
//	client := &http.Client{
//	    Transport: request.NewTransport(http.DefaultTransport, key),
//	}
//	// All requests through client are now signed.
//
// Server side (ConnectRPC & plain HTTP):
//
//	auth, err := request.NewAuthenticator(licensingClient, request.AuthOptions{...})
//	mux := http.NewServeMux()
//	mux.Handle("/", auth.Middleware(handler))
//
// Or in a handler:
//
//	inst, err := auth.Authenticate(r)
//
// The Authenticator calls the licensing client to look up the machine
// by fingerprint, extracts and verifies the public key, checks the
// signature, created window, nonce uniqueness, and license validity.
// The result is cached for a configurable TTL.
//
// ConnectRPC handlers are http.Handler implementations, so
// Middleware covers them directly — no separate interceptor is needed.
package request
