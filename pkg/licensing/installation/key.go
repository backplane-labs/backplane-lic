package installation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Key is an Ed25519 installation key pair. The private key is 64 bytes
// (seed + public key); the public key is 32 bytes.
type Key struct {
	priv ed25519.PrivateKey
}

// NewKey generates a fresh Ed25519 key pair from the system CSPRNG.
func NewKey() (Key, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return Key{}, fmt.Errorf("generate installation key: %w", err)
	}

	return Key{priv: priv}, nil
}

// ParseKey reconstructs a Key from its 32-byte seed.
func ParseKey(seed []byte) (Key, error) {
	if len(seed) != ed25519.SeedSize {
		return Key{}, ErrSeedSize
	}

	return Key{priv: ed25519.NewKeyFromSeed(seed)}, nil
}

// Seed returns the 32-byte seed that can reconstruct the key.
func (k Key) Seed() []byte {
	return k.priv.Seed()
}

// Public returns the 32-byte Ed25519 public key.
// The public key bytes are the last 32 bytes of the private key seed+pub
// encoding.
func (k Key) Public() ed25519.PublicKey {
	return ed25519.PublicKey(k.priv[ed25519.SeedSize:])
}

// Fingerprint returns the lowercase hex SHA-256 of the raw public key.
// It matches Fingerprint(k.Public()).
func (k Key) Fingerprint() string {
	return Fingerprint(k.Public())
}

// PrivateKey returns the underlying ed25519.PrivateKey.
func (k Key) PrivateKey() ed25519.PrivateKey {
	return k.priv
}

// Sign signs msg with the private key.
func (k Key) Sign(msg []byte) []byte {
	return ed25519.Sign(k.priv, msg)
}

// Fingerprint computes the installation fingerprint of a raw Ed25519
// public key: lowercase hex SHA-256.
func Fingerprint(pub ed25519.PublicKey) string {
	hash := sha256.Sum256(pub)

	return hex.EncodeToString(hash[:])
}

// fingerprintLen is the length of a fingerprint: hex-encoded SHA-256.
const fingerprintLen = 2 * sha256.Size

// ValidFingerprint reports whether s is a well-formed fingerprint:
// exactly 64 lowercase hex characters.
func ValidFingerprint(s string) bool {
	if len(s) != fingerprintLen {
		return false
	}

	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}
