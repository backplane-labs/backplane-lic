// Response signatures: the HTTP-signature-style headers the reference
// puts on every account-scoped response, negotiated through
// Keygen-Accept-Signature.
package crypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// The signature algorithms, in Keygen-Accept-Signature form.
const (
	AlgEd25519      = "ed25519"
	AlgRSAPSSSHA256 = "rsa-pss-sha256"
	AlgRSASHA256    = "rsa-sha256"
	AlgECDSAP256    = "ecdsa-p256"
)

// acceptSignatureRe accepts either part in either order (case-insensitive
// algorithm), like the reference's regex.
var acceptSignatureRe = regexp.MustCompile(`(?i)(?:keyid="(?P<keyid>[^"]*)"|algorithm="(?P<algorithm>[^"]*)")`)

// AcceptSignatureErrorKind discriminates the 400s the negotiation emits.
type AcceptSignatureErrorKind int

// The negotiation failures: malformed header, unknown algorithm, keyid
// for another account.
const (
	AcceptSignatureMalformed AcceptSignatureErrorKind = iota
	AcceptSignatureUnsupported
	AcceptSignatureKeyIDNotFound
)

// AcceptSignatureError reports why Keygen-Accept-Signature was rejected.
type AcceptSignatureError struct {
	Kind AcceptSignatureErrorKind
}

func (e *AcceptSignatureError) Error() string {
	switch e.Kind {
	case AcceptSignatureUnsupported:
		return "crypto: accept-signature: unsupported algorithm"
	case AcceptSignatureKeyIDNotFound:
		return "crypto: accept-signature: keyid not found"
	default:
		return "crypto: accept-signature: malformed"
	}
}

// ParseAcceptSignature resolves the response signature algorithm for a
// request: the header's algorithm (default ed25519), validated against
// the account (keyid must be the account id when present).
func ParseAcceptSignature(header string, accountID uuid.UUID) (string, error) {
	if header == "" {
		return AlgEd25519, nil
	}

	matches := acceptSignatureRe.FindAllStringSubmatch(header, -1)
	if len(matches) == 0 {
		return "", &AcceptSignatureError{Kind: AcceptSignatureMalformed}
	}

	keyidIdx := acceptSignatureRe.SubexpIndex("keyid")
	algIdx := acceptSignatureRe.SubexpIndex("algorithm")
	alg, keyid := "", ""

	for _, m := range matches {
		if m[keyidIdx] != "" {
			keyid = m[keyidIdx]
		}

		if m[algIdx] != "" {
			alg = m[algIdx]
		}
	}

	if alg == "" {
		alg = AlgEd25519
	}

	switch alg {
	case AlgEd25519, AlgRSAPSSSHA256, AlgRSASHA256, AlgECDSAP256:
	default:
		return "", &AcceptSignatureError{Kind: AcceptSignatureUnsupported}
	}

	if keyid != "" && keyid != accountID.String() {
		return "", &AcceptSignatureError{Kind: AcceptSignatureKeyIDNotFound}
	}

	return alg, nil
}

// Digest computes the Digest header value of a response body:
// sha-256=<strict base64 of the SHA-256>. An empty body hashes "".
func Digest(body []byte) string {
	sum := sha256.Sum256(body)

	return "sha-256=" + base64.StdEncoding.EncodeToString(sum[:])
}

// SigningData builds the signed string: the four lowercased-headers
// lines joined with newlines, over the ORIGINAL request path (with
// query) and the host without port.
func SigningData(method, requestPath, host, date, digest string) string {
	return strings.Join([]string{
		"(request-target): " + strings.ToLower(method) + " " + requestPath,
		"host: " + host,
		"date: " + date,
		"digest: " + digest,
	}, "\n")
}

// Sign signs the signing data with the account's private key of the
// selected algorithm and returns the strict-base64 signature.
func Sign(keys AccountKeys, alg, signingData string) (string, error) {
	switch alg {
	case AlgEd25519:
		seed, err := hex.DecodeString(keys.Ed25519Private)
		if err != nil {
			return "", fmt.Errorf("crypto: sign: ed25519 seed: %w", err)
		}

		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), []byte(signingData))

		return base64.StdEncoding.EncodeToString(sig), nil

	case AlgRSAPSSSHA256, AlgRSASHA256:
		key, err := parseRSAPrivate(keys.RSAPrivate)
		if err != nil {
			return "", err
		}

		hashed := sha256.Sum256([]byte(signingData))

		var sig []byte

		if alg == AlgRSAPSSSHA256 {
			// salt_length: :max (k - h - 2), like the reference.
			const saltLen = 256 - sha256.Size - 2

			sig, err = rsa.SignPSS(rand.Reader, key, crypto.SHA256, hashed[:], &rsa.PSSOptions{SaltLength: saltLen})
		} else {
			sig, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
		}

		if err != nil {
			return "", fmt.Errorf("crypto: sign: rsa %s: %w", alg, err)
		}

		return base64.StdEncoding.EncodeToString(sig), nil

	case AlgECDSAP256:
		key, err := parseECDSAPrivate(keys.ECDSAPrivate)
		if err != nil {
			return "", err
		}

		hashed := sha256.Sum256([]byte(signingData))

		sig, err := ecdsa.SignASN1(rand.Reader, key, hashed[:])
		if err != nil {
			return "", fmt.Errorf("crypto: sign: ecdsa: %w", err)
		}

		return base64.StdEncoding.EncodeToString(sig), nil

	default:
		return "", fmt.Errorf("crypto: sign: %w: %s", errUnsupportedAlgorithm, alg)
	}
}

// errUnsupportedAlgorithm is a sentinel for Sign's default branch.
var errUnsupportedAlgorithm = errors.New("unsupported algorithm")

func parseRSAPrivate(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("crypto: sign: rsa private key: %w", errNoPEM)
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: sign: rsa private key: %w", err)
	}

	return key, nil
}

func parseECDSAPrivate(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("crypto: sign: ecdsa private key: %w", errNoPEM)
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: sign: ecdsa private key: %w", err)
	}

	return key, nil
}

var errNoPEM = errors.New("no PEM block")
