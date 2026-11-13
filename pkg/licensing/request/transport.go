package request

import (
	"fmt"
	"net/http"

	"github.com/yaronf/httpsign"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

// Transport is an http.RoundTripper that signs every outgoing request
// with RFC 9421 HTTP Message Signatures (Ed25519) via
// github.com/yaronf/httpsign. It wraps a base transport —
// http.DefaultTransport when nil.
//
// Signed requests carry the Signature and Signature-Input headers with
// the label "sig1"; requests with a body also carry a sha-256
// Content-Digest header, which is covered by the signature.
type Transport struct {
	base        http.RoundTripper
	key         installation.Key
	maxBodySize int64
}

// TransportOption configures a Transport.
type TransportOption func(*Transport)

// WithMaxBodySize caps the request body the Transport buffers to
// compute the Content-Digest header. Zero or negative means no limit.
func WithMaxBodySize(maxBytes int64) TransportOption {
	return func(t *Transport) { t.maxBodySize = maxBytes }
}

// NewTransport returns a Transport that signs requests with key. The
// keyid signature parameter is the key's fingerprint.
func NewTransport(base http.RoundTripper, key installation.Key, opts ...TransportOption) *Transport {
	t := &Transport{base: base, key: key}

	for _, o := range opts {
		o(t)
	}

	return t
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	if err := t.setDigest(req); err != nil {
		return nil, err
	}

	if err := t.sign(req); err != nil {
		return nil, err
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	res, err := base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("signed request: %w", err)
	}

	return res, nil
}

// setDigest computes the Content-Digest header (RFC 9530, sha-256) for
// requests with a body. The header is set before signing so the
// signature covers it.
func (t *Transport) setDigest(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}

	digestOpts := httpsign.NewDigestOptions().SetMaxBodySize(t.maxBodySize)

	header, err := httpsign.GenerateContentDigestHeader(&req.Body, []string{httpsign.DigestSha256}, digestOpts)
	if err != nil {
		return fmt.Errorf("compute content digest: %w", err)
	}

	req.Header.Set(headerContentDigest, header)

	return nil
}

// sign adds the Signature and Signature-Input headers to req with a
// fresh created timestamp and nonce.
func (t *Transport) sign(req *http.Request) error {
	nonce, err := generateNonce()
	if err != nil {
		return err
	}

	config := httpsign.NewSignConfig().
		SetKeyID(t.key.Fingerprint()).
		SetNonce(nonce)

	signer, err := httpsign.NewEd25519Signer(t.key.PrivateKey(), config, signedFields())
	if err != nil {
		return fmt.Errorf("create signer: %w", err)
	}

	sigInput, sig, err := httpsign.SignRequest(sigLabel, *signer, req)
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	req.Header.Set("Signature", sig)
	req.Header.Set("Signature-Input", sigInput)

	return nil
}
