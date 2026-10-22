package licensing

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Signature algorithms, in Keygen's wire form (the part after '+' in a
// certificate algorithm and in the Keygen-Signature header).
const (
	algEd25519      = "ed25519"
	algRSAPSSSHA256 = "rsa-pss-sha256"
	algRSASHA256    = "rsa-sha256"
	algECDSAP256    = "ecdsa-p256"
)

// Verifier verifies Keygen signatures: API responses, webhooks, license
// and machine files, and cryptographically signed license keys. It is
// immutable after construction; the zero value refuses everything with
// ErrPublicKeyMissing.
type Verifier struct {
	ed25519Key ed25519.PublicKey
	rsaKey     *rsa.PublicKey
	ecdsaKey   *ecdsa.PublicKey
}

// NewVerifier parses a public key: a 64-character hex ed25519 key (the
// form Keygen accounts expose), or a PEM-encoded RSA or ECDSA public key.
func NewVerifier(publicKey string) (Verifier, error) {
	if publicKey == "" {
		return Verifier{}, ErrPublicKeyMissing
	}

	if key, err := parseEd25519Hex(publicKey); err == nil {
		return Verifier{ed25519Key: key}, nil
	}

	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		return Verifier{}, fmt.Errorf("%w: neither ed25519 hex nor PEM", ErrPublicKeyInvalid)
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return Verifier{}, fmt.Errorf("%w: %w", ErrPublicKeyInvalid, err)
	}

	verifier := Verifier{}

	switch key := parsed.(type) {
	case *rsa.PublicKey:
		verifier.rsaKey = key
	case *ecdsa.PublicKey:
		verifier.ecdsaKey = key
	default:
		return Verifier{}, fmt.Errorf("%w: unsupported key type %T", ErrPublicKeyInvalid, key)
	}

	return verifier, nil
}

func parseEd25519Hex(publicKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return nil, fmt.Errorf("%w: not hex", ErrPublicKeyInvalid)
	}

	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: wrong ed25519 key size", ErrPublicKeyInvalid)
	}

	return ed25519.PublicKey(raw), nil
}

// configured reports whether any key was parsed.
func (v Verifier) configured() bool {
	return v.ed25519Key != nil || v.rsaKey != nil || v.ecdsaKey != nil
}

// verify checks sig over msg with the key matching the wire algorithm.
func (v Verifier) verify(algorithm string, msg, sig []byte) error {
	switch algorithm {
	case algEd25519:
		if v.ed25519Key == nil {
			return fmt.Errorf("%w: ed25519 key not configured", ErrPublicKeyMissing)
		}

		if !ed25519.Verify(v.ed25519Key, msg, sig) {
			return ErrSignatureInvalid
		}
	case algRSAPSSSHA256:
		if v.rsaKey == nil {
			return fmt.Errorf("%w: rsa key not configured", ErrPublicKeyMissing)
		}

		digest := sha256.Sum256(msg)

		// Salt length auto: Keygen signs PSS with the maximum salt
		// (OpenSSL's salt_length: :max), so the verifier must accept
		// any salt length the signer chose.
		pss := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto}

		if err := rsa.VerifyPSS(v.rsaKey, crypto.SHA256, digest[:], sig, pss); err != nil {
			return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
		}
	case algRSASHA256:
		if v.rsaKey == nil {
			return fmt.Errorf("%w: rsa key not configured", ErrPublicKeyMissing)
		}

		digest := sha256.Sum256(msg)

		if err := rsa.VerifyPKCS1v15(v.rsaKey, crypto.SHA256, digest[:], sig); err != nil {
			return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
		}
	case algECDSAP256:
		if v.ecdsaKey == nil {
			return fmt.Errorf("%w: ecdsa key not configured", ErrPublicKeyMissing)
		}

		digest := sha256.Sum256(msg)

		if !ecdsa.VerifyASN1(v.ecdsaKey, digest[:], sig) {
			return ErrSignatureInvalid
		}
	default:
		return fmt.Errorf("%w: %s", ErrSchemeNotSupported, algorithm)
	}

	return nil
}

// VerifyResponse checks the Keygen-Signature headers of an API response
// against its body. It verifies the Digest header against the body, the
// Date header against skew, and the signature over the HTTP signing
// string "(request-target) host date digest". request is the request the
// response answers (its method, URI, and Host header define the signed
// request-target).
func (v Verifier) VerifyResponse(request *http.Request, header http.Header, body []byte, skew SkewGuard) error {
	if !v.configured() {
		return ErrPublicKeyMissing
	}

	digest, err := verifyDigest(header, body)
	if err != nil {
		return err
	}

	date, err := verifyDate(header, skew)
	if err != nil {
		return err
	}

	target := requestTarget(request.Method, request.URL.RequestURI())
	host := requestHost(request)

	return v.verifySignedMessage(target, host, date, digest, header.Get("Keygen-Signature"))
}

// VerifyWebhook checks the Keygen-Signature headers of an incoming
// webhook request. The event must be considered untrusted when it
// returns an error. The request body is replaced with an equivalent
// reader after inspection.
func (v Verifier) VerifyWebhook(request *http.Request, skew SkewGuard) error {
	if !v.configured() {
		return ErrPublicKeyMissing
	}

	body, err := readBody(request)
	if err != nil {
		return fmt.Errorf("licensing: webhook: read body: %w", err)
	}

	digest, err := verifyDigest(request.Header, body)
	if err != nil {
		return err
	}

	date, err := verifyDate(request.Header, skew)
	if err != nil {
		return err
	}

	target := requestTarget(request.Method, request.URL.RequestURI())
	host := requestHost(request)

	return v.verifySignedMessage(target, host, date, digest, request.Header.Get("Keygen-Signature"))
}

// verifySignedMessage assembles and checks the HTTP signing string.
func (v Verifier) verifySignedMessage(target, host, date, digest, sigHeader string) error {
	if sigHeader == "" {
		return ErrSignatureMissing
	}

	params := parseSignatureHeader(sigHeader)

	sig, err := base64.StdEncoding.DecodeString(params["signature"])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
	}

	msg := fmt.Sprintf(
		"(request-target): %s\nhost: %s\ndate: %s\ndigest: %s",
		target, host, date, digest,
	)

	return v.verify(params["algorithm"], []byte(msg), sig)
}

// readBody drains the request body and replaces it with an equivalent
// reader so later handlers can read it again.
func readBody(request *http.Request) ([]byte, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read webhook body: %w", err)
	}

	if err := request.Body.Close(); err != nil {
		return nil, fmt.Errorf("close webhook body: %w", err)
	}

	request.Body = io.NopCloser(bytes.NewBuffer(body))

	return body, nil
}

// VerifyLicenseKey verifies a cryptographically signed license key and
// returns its embedded dataset (the JSON the key was signed over).
// Supported schemes: ED25519_SIGN, ECDSA_P256_SIGN, and the RSA
// RSA_2048_PKCS1[PSS]_SIGN[_V2] variants.
func (v Verifier) VerifyLicenseKey(scheme Scheme, key string) ([]byte, error) {
	encDataset, encSig, err := splitSignedKey(key)
	if err != nil {
		return nil, err
	}

	// v2 schemes and the prefix-based schemes sign "key/"+enc; v1 RSA
	// schemes sign the raw enc.
	msg := []byte(keyPrefix + encDataset)
	algorithm, prefixSigned := keySchemeAlgorithm(scheme)

	if !prefixSigned {
		msg = []byte(encDataset)
	}

	sig, err := decodeURLBase64(encSig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLicenseKeyNotGenuine, err)
	}

	if err := v.verify(algorithm, msg, sig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLicenseKeyNotGenuine, err)
	}

	dataset, err := decodeURLBase64(encDataset)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLicenseKeyNotGenuine, err)
	}

	return dataset, nil
}

// keyPrefix is prepended to the encoded dataset before signing.
const keyPrefix = "key/"

// splitSignedKey splits "key/<enc>.<sig>" or "<enc>.<sig>".
func splitSignedKey(key string) (string, string, error) {
	beforeSig, sig, found := strings.Cut(key, ".")
	if !found {
		return "", "", fmt.Errorf("%w: malformed key", ErrLicenseKeyNotGenuine)
	}

	dataset := strings.TrimPrefix(beforeSig, keyPrefix)

	if dataset == "" || sig == "" {
		return "", "", fmt.Errorf("%w: malformed key", ErrLicenseKeyNotGenuine)
	}

	return dataset, sig, nil
}

// keySchemeAlgorithm maps a license scheme to its wire signing algorithm
// and whether the signed message carries the key/ prefix.
func keySchemeAlgorithm(scheme Scheme) (string, bool) {
	switch scheme {
	case SchemeEd25519:
		return algEd25519, true
	case SchemeECDSAP256:
		return algECDSAP256, true
	case SchemeRSAPKCS1V2:
		return algRSASHA256, true
	case SchemeRSAPSSV2:
		return algRSAPSSSHA256, true
	case SchemeRSAPKCS1:
		return algRSASHA256, false
	case SchemeRSAPSS:
		return algRSAPSSSHA256, false
	default:
		return "", true
	}
}

// decodeURLBase64 decodes URL-safe base64 with or without padding.
func decodeURLBase64(value string) ([]byte, error) {
	decoded, err := base64.URLEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode base64url: %w", err)
	}

	return raw, nil
}

// requestTarget renders "<method> <path>" for the signing string.
func requestTarget(method, uri string) string {
	return strings.ToLower(method) + " " + uri
}

// requestHost extracts the host the server saw: the Host header without
// a port.
func requestHost(request *http.Request) string {
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}

	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = withoutPort
	}

	return host
}

// parseSignatureHeader splits a Keygen-Signature header into its
// parameters: keyid, algorithm, signature, headers.
func parseSignatureHeader(header string) map[string]string {
	params := make(map[string]string)

	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}

		params[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	return params
}

// verifyDigest checks the sha-256 Digest header against the body and
// returns the digest value as signed.
func verifyDigest(header http.Header, body []byte) (string, error) {
	digestHeader := header.Get("Digest")
	if digestHeader == "" {
		return "", ErrDigestMissing
	}

	shasum := sha256.Sum256(body)
	digest := "sha-256=" + base64.StdEncoding.EncodeToString(shasum[:])

	if digest != digestHeader {
		return "", ErrDigestInvalid
	}

	return digest, nil
}

// verifyDate parses the Date header, checks it against the skew guard,
// and returns the header value as signed.
func verifyDate(header http.Header, skew SkewGuard) (string, error) {
	dateHeader := header.Get("Date")
	if dateHeader == "" {
		return "", ErrDateMissing
	}

	t, err := http.ParseTime(dateHeader)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDateInvalid, err)
	}

	if !skew.Within(t) {
		return "", ErrDateTooOld
	}

	return dateHeader, nil
}

// SkewGuard decides whether a timestamp is close enough to now. The zero
// value accepts everything.
type SkewGuard struct {
	max time.Duration
}

// newSkewGuard builds a guard tolerating skew; non-positive skew disables
// the check.
func SkewGuardFor(skew time.Duration) SkewGuard {
	return SkewGuard{max: skew}
}

// Within reports whether t is not older than skew (future dates always
// pass, matching Keygen clients).
func (g SkewGuard) Within(t time.Time) bool {
	if g.max <= 0 {
		return true
	}

	return !t.Before(time.Now().Add(-g.max))
}
