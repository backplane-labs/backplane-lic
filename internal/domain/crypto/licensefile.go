// License (and machine) file certificates: the {enc, sig, alg}
// document wrapped in the PEM-like block the reference's checkout
// services emit.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The certificate's encoding algorithms (the signature algorithms are
// the Alg* constants of signature.go).
const (
	EncodingAES256GCM = "aes-256-gcm"
	EncodingBase64    = "base64"
)

// algParts is the <enc>+<sig> pair part count.
const algParts = 2

// defaultTTLSeconds is the checkout TTL default (1 month).
const defaultTTLSeconds = 2629746

// minTTLSeconds is the checkout TTL floor (1 hour).
const minTTLSeconds = 3600

// FileAlgorithmError reports an invalid checkout algorithm; the
// message is the reference's verbatim detail.
type FileAlgorithmError struct{ Detail string }

func (e *FileAlgorithmError) Error() string { return "crypto: license file: " + e.Detail }

// FileTTLError reports an invalid checkout TTL.
type FileTTLError struct{}

func (e *FileTTLError) Error() string { return "crypto: license file: invalid ttl" }

// ResolveFileAlgorithm validates the checkout parameters: the TTL
// floor, the explicit algorithm (or the derived default) and its parts.
// signingDefault is the scheme-derived signature algorithm ("ed25519"
// for plain/legacy policies).
func ResolveFileAlgorithm(algorithm string, encrypt bool, signingDefault string, ttl *int32) (string, error) {
	if ttl != nil && *ttl < minTTLSeconds {
		return "", &FileTTLError{}
	}

	if algorithm != "" && strings.TrimSpace(algorithm) == "" {
		return "", &FileAlgorithmError{Detail: "algorithm must be present"}
	}

	if algorithm == "" {
		encoding := EncodingBase64
		if encrypt {
			encoding = EncodingAES256GCM
		}

		return encoding + "+" + signingDefault, nil
	}

	parts := strings.SplitN(algorithm, "+", algParts)
	if len(parts) != algParts {
		return "", &FileAlgorithmError{Detail: "invalid encoding algorithm"}
	}

	switch parts[0] {
	case EncodingAES256GCM, EncodingBase64:
	default:
		return "", &FileAlgorithmError{Detail: "invalid encoding algorithm"}
	}

	switch parts[1] {
	case AlgEd25519, AlgRSAPSSSHA256, AlgRSASHA256, AlgECDSAP256:
	default:
		return "", &FileAlgorithmError{Detail: "invalid signing algorithm"}
	}

	return algorithm, nil
}

// DefaultTTL answers the checkout default TTL in seconds.
func DefaultTTL() int32 { return defaultTTLSeconds }

// MinTTL answers the checkout TTL floor in seconds.
func MinTTL() int32 { return minTTLSeconds }

// SchemeSigningAlgorithm answers the default checkout signature
// algorithm of a policy scheme (true — the plain default — becomes
// ed25519).
func SchemeSigningAlgorithm(scheme string) string {
	switch scheme {
	case SchemePKCS1PSSV2, "RSA_2048_PKCS1_PSS_SIGN":
		return AlgRSAPSSSHA256
	case SchemePKCS1SignV2, "RSA_2048_PKCS1_SIGN",
		SchemePKCS1Encrypt, SchemeJWT:
		return AlgRSASHA256
	case "ED25519_SIGN":
		return AlgEd25519
	case "ECDSA_P256_SIGN":
		return AlgECDSAP256
	default:
		return AlgEd25519
	}
}

// FileCertificateInput carries the rendered checkout payload and its
// encryption/signing parameters.
type FileCertificateInput struct {
	Keys AccountKeys
	// Payload is the JSON:API dataset the certificate carries.
	Payload []byte
	// Secret is the encryption key material (the license key, or the
	// license key + fingerprint for machine files).
	Secret string
	// Algorithm is the resolved <enc>+<sig> pair.
	Algorithm string
	// RNG feeds the AES-GCM nonce.
	RNG io.Reader
	// Prefix is the signed string's prefix ("license/" or "machine/").
	Prefix string
	// Begin/End are the certificate block markers.
	Begin, End string
}

// BuildFileCertificate encodes, signs and wraps the payload into the
// certificate block, answering the block and the signature.
func BuildFileCertificate(in FileCertificateInput) (string, error) {
	parts := strings.SplitN(in.Algorithm, "+", algParts)
	if len(parts) != algParts {
		return "", &FileAlgorithmError{Detail: "invalid encoding algorithm"}
	}

	encoding, signing := parts[0], parts[1]

	var enc string

	if encoding == EncodingAES256GCM {
		encrypted, err := encryptGCM(in.RNG, in.Payload, in.Secret)
		if err != nil {
			return "", err
		}

		enc = encrypted
	} else {
		enc = base64.StdEncoding.EncodeToString(in.Payload)
	}

	sig, err := Sign(in.Keys, signing, in.Prefix+enc)
	if err != nil {
		return "", err
	}

	doc, err := json.Marshal(map[string]string{"enc": enc, "sig": sig, "alg": in.Algorithm})
	if err != nil {
		return "", fmt.Errorf("crypto: license file: certificate: %w", err)
	}

	wrapped := wrap60(base64.StdEncoding.EncodeToString(doc))

	return in.Begin + "\n" + wrapped + "\n" + in.End + "\n", nil
}

// encryptGCM encrypts with AES-256-GCM over SHA256(secret), answering
// the ciphertext.iv.tag triple of strict-base64 parts.
func encryptGCM(rng io.Reader, payload []byte, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("crypto: license file: aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: license file: gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rng, nonce); err != nil {
		return "", fmt.Errorf("crypto: license file: nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, payload, nil)
	ciphertext, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]

	parts := []string{
		base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(tag),
	}

	return strings.Join(parts, "."), nil
}

// lineWrap is the certificate's base64 line width.
const lineWrap = 60

// wrap60 line-wraps base64 at 60 characters, like Ruby's encode64.
func wrap60(s string) string {
	var lines []string

	for i := 0; i < len(s); i += lineWrap {
		end := i + lineWrap
		if end > len(s) {
			end = len(s)
		}

		lines = append(lines, s[i:end])
	}

	return strings.Join(lines, "\n")
}

// FileAlgorithmEncrypted answers whether the resolved algorithm's
// encoding encrypts (aes-256-gcm).
func FileAlgorithmEncrypted(algorithm string) bool {
	return strings.HasPrefix(algorithm, EncodingAES256GCM+"+")
}
