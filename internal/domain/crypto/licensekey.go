// License-key generation: the plain random keys, the legacy encrypted
// keys and the scheme keys (seed JSON signed or encrypted with the
// account's key material), exactly like the reference's License model
// callbacks.
package crypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// The crypto scheme names shared by the key and file builders.
const (
	SchemePKCS1Encrypt = "RSA_2048_PKCS1_ENCRYPT"
	SchemeJWT          = "RSA_2048_JWT_RS256"
	SchemePKCS1SignV2  = "RSA_2048_PKCS1_SIGN_V2"
	SchemePKCS1PSSV2   = "RSA_2048_PKCS1_PSS_SIGN_V2"
)

// rsaMaxByteSize is the RSA_2048_PKCS1_ENCRYPT seed limit
// (RSA_KEY_SIZE/8 − 11, like the reference).
const rsaMaxByteSize = 245

// The version suffixes of generated keys, their length and the hex
// pair factor.
const (
	versionPlain  = "v3"
	versionLegacy = "v1"
	versionLen    = 2
	hexPair       = 2
)

// uuidHexLength is the UUID length without dashes (the legacy
// encrypted key's grouping).
const uuidHexLength = 32

// plainHexLength is the plain key's hex length before the version
// suffix substitution (SecureRandom.hex(16)).
const plainHexLength = 32

// legacyHexLength is the legacy encrypted key's hex length before
// substitution (SecureRandom.hex(64)).
const legacyHexLength = 128

// KeyInput carries what a generated license key needs: the license row
// (with its joined policy and owner) and the account keys.
type KeyInput struct {
	Keys       AccountKeys
	AccountID  uuid.UUID
	ProductID  uuid.UUID
	Policy     PolicyKeyFacts
	LicenseID  uuid.UUID
	OwnerID    uuid.NullUUID
	OwnerEmail *string
	CreatedAt  time.Time
	// Expiry is the computed expiry (nil for perpetual).
	Expiry *time.Time
	// RNG feeds the random keys (tests inject a deterministic one).
	RNG io.Reader
}

// PolicyKeyFacts is the policy slice the seed key templates carry.
type PolicyKeyFacts struct {
	ID        uuid.UUID
	Duration  *int32
	Scheme    string
	Encrypted bool
}

// PlainKey generates the default unencrypted key: hex(16) with the last
// two characters replaced by "v3", split into 6-character groups
// joined with dashes and uppercased (e.g. A1B2C3-…-V3).
func PlainKey(rng io.Reader) (string, error) {
	raw := make([]byte, plainHexLength/hexPair)
	if _, err := io.ReadFull(rng, raw); err != nil {
		return "", fmt.Errorf("crypto: license key: %w", err)
	}

	token := hex.EncodeToString(raw)
	token = token[:plainHexLength-versionLen] + versionPlain

	return strings.ToUpper(chunk(token, 6)), nil //nolint:mnd // the reference's literal grouping
}

// GenerateKey produces the license key for the input's policy scheme:
// the plain random key for schemeless policies, the bcrypt-hyphen
// format for LEGACY_ENCRYPT (answering the one-time raw plaintext),
// and the seed-key construction signed or encrypted per scheme.
func GenerateKey(in KeyInput) (string, *string, error) {
	switch {
	case in.Policy.Encrypted && in.Policy.Scheme == "LEGACY_ENCRYPT":
		return legacyEncryptedKey(in.RNG, in.LicenseID)
	case in.Policy.Scheme == "":
		key, err := PlainKey(in.RNG)

		return key, nil, err
	default:
		key, err := SchemeKey(in, SeedKey(in, ""))

		return key, nil, err
	}
}

// CompareLegacyKey checks a raw legacy key against the stored bcrypt
// digest (the lookup service's compare_hashed_token).
func CompareLegacyKey(storedDigest, raw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(storedDigest), []byte(sha256Hex(raw))) == nil
}

// SeedKey builds the seed of a scheme policy's key: the custom key
// template substituted, or the default seed JSON. The caller applies
// it with SchemeKey.
func SeedKey(in KeyInput, custom string) string {
	if custom != "" {
		return formatTemplate(custom, in)
	}

	switch in.Policy.Scheme {
	case SchemePKCS1Encrypt:
		return marshal(map[string]any{
			"id":       in.LicenseID,
			"created":  iso8601ms(in.CreatedAt),
			"duration": in.Policy.Duration,
			"expiry":   timeOrNil(in.Expiry),
		})

	case SchemeJWT:
		claims := map[string]any{
			"jti": uuid.New().String(),
			"iss": "https://keygen.sh",
			"aud": in.AccountID.String(),
			"sub": in.LicenseID.String(),
			"iat": in.CreatedAt.Unix(),
			"nbf": in.CreatedAt.Unix(),
		}
		if in.Expiry != nil {
			claims["exp"] = in.Expiry.Unix()
		}

		return marshal(claims)

	default:
		user := any(nil)

		if in.OwnerID.Valid {
			email := ""
			if in.OwnerEmail != nil {
				email = *in.OwnerEmail
			}

			user = map[string]any{"id": in.OwnerID.UUID.String(), "email": email}
		}

		return marshal(map[string]any{
			"account": map[string]any{"id": in.AccountID.String()},
			"product": map[string]any{"id": in.ProductID.String()},
			"policy": map[string]any{
				"id":       in.Policy.ID.String(),
				"duration": in.Policy.Duration,
			},
			"user": user,
			"license": map[string]any{
				"id":      in.LicenseID.String(),
				"created": iso8601ms(in.CreatedAt),
				"expiry":  timeOrNil(in.Expiry),
			},
		})
	}
}

// SchemeKey crypts a seed (the custom key or the default seed JSON)
// into the wire key of the input's scheme.
func SchemeKey( //nolint:cyclop,funlen // one branch per scheme
	in KeyInput, seed string,
) (string, error) {
	b64 := func(b []byte) string { return base64.URLEncoding.EncodeToString(b) }

	switch in.Policy.Scheme {
	case SchemePKCS1Encrypt:
		if len(seed) > rsaMaxByteSize {
			return "", ErrKeyTooLarge
		}

		priv, err := parseRSAPrivate(in.Keys.RSAPrivate)
		if err != nil {
			return "", err
		}

		// Ruby's private_encrypt is PKCS#1 v1.5 with the private key:
		// Go's SignPKCS1v15 with no hash over the raw message.
		enc, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.Hash(0), []byte(seed))
		if err != nil {
			return "", fmt.Errorf("crypto: license key: pkcs1 encrypt: %w", err)
		}

		return b64(enc), nil

	case "RSA_2048_PKCS1_SIGN", SchemePKCS1SignV2:
		priv, err := parseRSAPrivate(in.Keys.RSAPrivate)
		if err != nil {
			return "", err
		}

		// v1 signs the raw seed and renders it encoded; v2 signs the
		// prefixed "key/<seed>" form and renders that.
		signed, rendered := seed, b64([]byte(seed))
		if in.Policy.Scheme == SchemePKCS1SignV2 {
			rendered = "key/" + b64([]byte(seed))
			signed = rendered
		}

		hashed := sha256.Sum256([]byte(signed))

		sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hashed[:])
		if err != nil {
			return "", fmt.Errorf("crypto: license key: pkcs1 sign: %w", err)
		}

		return rendered + "." + b64(sig), nil

	case "RSA_2048_PKCS1_PSS_SIGN", SchemePKCS1PSSV2:
		priv, err := parseRSAPrivate(in.Keys.RSAPrivate)
		if err != nil {
			return "", err
		}

		signed, rendered := seed, b64([]byte(seed))
		if in.Policy.Scheme == SchemePKCS1PSSV2 {
			rendered = "key/" + b64([]byte(seed))
			signed = rendered
		}

		hashed := sha256.Sum256([]byte(signed))

		const saltLen = 256 - sha256.Size - 2

		sig, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA256, hashed[:],
			&rsa.PSSOptions{SaltLength: saltLen})
		if err != nil {
			return "", fmt.Errorf("crypto: license key: pss sign: %w", err)
		}

		return rendered + "." + b64(sig), nil

	case SchemeJWT:
		return jwtRS256(in, seed)

	case "ED25519_SIGN":
		seedBytes, err := hex.DecodeString(in.Keys.Ed25519Private)
		if err != nil {
			return "", fmt.Errorf("crypto: license key: ed25519 seed: %w", err)
		}

		data := "key/" + b64([]byte(seed))
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seedBytes), []byte(data))

		return data + "." + b64(sig), nil

	case "ECDSA_P256_SIGN":
		priv, err := parseECDSAPrivate(in.Keys.ECDSAPrivate)
		if err != nil {
			return "", err
		}

		data := "key/" + b64([]byte(seed))
		hashed := sha256.Sum256([]byte(data))

		sig, err := ecdsa.SignASN1(rand.Reader, priv, hashed[:])
		if err != nil {
			return "", fmt.Errorf("crypto: license key: ecdsa sign: %w", err)
		}

		return data + "." + b64(sig), nil

	default:
		return "", fmt.Errorf("crypto: license key: %w: %s", errUnsupportedAlgorithm, in.Policy.Scheme)
	}
}

func jwtRS256(in KeyInput, claimsJSON string) (string, error) {
	priv, err := parseRSAPrivate(in.Keys.RSAPrivate)
	if err != nil {
		return "", err
	}

	var claims map[string]any
	if err := json.Unmarshal([]byte(claimsJSON), &claims); err != nil {
		return "", ErrJWTClaimsInvalid
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(claimsJSON))

	hashed := sha256.Sum256([]byte(header + "." + payload))

	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("crypto: license key: jwt sign: %w", err)
	}

	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func legacyEncryptedKey(rng io.Reader, licenseID uuid.UUID) (string, *string, error) {
	buf := make([]byte, legacyHexLength/hexPair)
	if _, err := io.ReadFull(rng, buf); err != nil {
		return "", nil, fmt.Errorf("crypto: license key: legacy: %w", err)
	}

	raw := hex.EncodeToString(buf)
	raw = raw[:legacyHexLength-versionLen] + versionLegacy
	// The first 32 characters carry the license id (sans dashes) so the
	// lookup service can find the row; 32-character groups follow.
	raw = strings.ReplaceAll(licenseID.String(), "-", "") + raw[uuidHexLength:]
	raw = chunk(raw, uuidHexLength)

	digest, err := bcrypt.GenerateFromPassword([]byte(sha256Hex(raw)), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, fmt.Errorf("crypto: license key: legacy: %w", err)
	}

	return string(digest), &raw, nil
}

// formatTemplate substitutes the {{account}} {{product}} {{policy}}
// {{user}} {{email}} {{created}} {{expiry}} {{duration}} {{id}}
// variables of a custom key (TemplateFormatService).
func formatTemplate(template string, in KeyInput) string {
	created := iso8601ms(in.CreatedAt)

	expiry := ""
	if in.Expiry != nil {
		expiry = iso8601ms(*in.Expiry)
	}

	duration := ""
	if in.Policy.Duration != nil {
		duration = strconv.Itoa(int(*in.Policy.Duration))
	}

	email := ""
	if in.OwnerEmail != nil {
		email = *in.OwnerEmail
	}

	pairs := []string{
		"account", in.AccountID.String(),
		"product", in.ProductID.String(),
		"policy", in.Policy.ID.String(),
		"user", in.OwnerID.UUID.String(),
		"email", email,
		"created", created,
		"expiry", expiry,
		"duration", duration,
		"id", in.LicenseID.String(),
	}

	out := template
	for i := 0; i+1 < len(pairs); i += algParts {
		out = strings.ReplaceAll(out, "{{"+pairs[i]+"}}", pairs[i+1])
	}

	return out
}

// ErrKeyTooLarge: the RSA_2048_PKCS1_ENCRYPT seed exceeds the limit.
var ErrKeyTooLarge = fmt.Errorf( //nolint:goerr113 // a domain sentinel
	"crypto: license key: key exceeds maximum byte length (max size of %d bytes)", rsaMaxByteSize,
)

// ErrJWTClaimsInvalid: the custom JWT seed is not valid claims JSON.
var ErrJWTClaimsInvalid = errors.New("crypto: license key: key is not a valid JWT claims payload")

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

func chunk(s string, size int) string {
	var groups []string

	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}

		groups = append(groups, s[i:end])
	}

	return strings.Join(groups, "-")
}

func iso8601ms(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}

	return iso8601ms(*t)
}

func marshal(m map[string]any) string {
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}

	return string(raw)
}
