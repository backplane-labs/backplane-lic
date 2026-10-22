// Package crypto holds the key generation and signing helpers: account
// keys, license-key schemes, license/machine files, response signatures
// and token secrets/digests. No infrastructure here.
package crypto

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
)

// rsaKeySize of the account's RSA keypair, like Keygen's RSA_KEY_SIZE;
// secretKeyBytes of its secret key (hex-encoded to 128 chars).
const (
	rsaKeySize     = 2048
	secretKeyBytes = 64
)

// AccountKeys is the account's key material. Storage formats follow
// Keygen's: the secret key and both ed25519 keys are hex strings, the RSA
// and ECDSA keys are PEM.
type AccountKeys struct {
	// SecretKey hex-encodes 64 random bytes; v3 token digests are
	// HMAC-SHA256 over it.
	SecretKey string
	// RSAPrivate/RSAPublic are PKCS#1 PEM ("RSA PRIVATE KEY" /
	// "RSA PUBLIC KEY"), 2048 bits.
	RSAPrivate string
	RSAPublic  string
	// Ed25519Private hex-encodes the 32-byte seed; Ed25519Public the
	// 32-byte public key.
	Ed25519Private string
	Ed25519Public  string
	// ECDSAPrivate is a SEC1 PEM ("EC PRIVATE KEY"), ECDSAPublic an SPKI
	// PEM, both P-256.
	ECDSAPrivate string
	ECDSAPublic  string
}

// GenerateAccountKeys generates a full set of account keys from rng (64
// random bytes of secret, RSA-2048, ed25519, ECDSA P-256).
func GenerateAccountKeys(rng io.Reader) (AccountKeys, error) {
	secret := make([]byte, secretKeyBytes)
	if _, err := io.ReadFull(rng, secret); err != nil {
		return AccountKeys{}, fmt.Errorf("crypto: account secret: %w", err)
	}

	rsaKey, err := rsa.GenerateKey(rng, rsaKeySize)
	if err != nil {
		return AccountKeys{}, fmt.Errorf("crypto: account rsa key: %w", err)
	}

	edPub, edPriv, err := ed25519.GenerateKey(rng)
	if err != nil {
		return AccountKeys{}, fmt.Errorf("crypto: account ed25519 key: %w", err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rng)
	if err != nil {
		return AccountKeys{}, fmt.Errorf("crypto: account ecdsa key: %w", err)
	}

	rsaPrivPEM := pemPKCS1Private(rsaKey)
	rsaPubPEM := pemPKCS1Public(&rsaKey.PublicKey)

	ecPrivPEM, err := pemSEC1Private(ecKey)
	if err != nil {
		return AccountKeys{}, err
	}

	ecPubPEM, err := pemSPKIPublic(&ecKey.PublicKey)
	if err != nil {
		return AccountKeys{}, err
	}

	return AccountKeys{
		SecretKey:      hex.EncodeToString(secret),
		RSAPrivate:     rsaPrivPEM,
		RSAPublic:      rsaPubPEM,
		Ed25519Private: hex.EncodeToString(edPriv.Seed()),
		Ed25519Public:  hex.EncodeToString(edPub),
		ECDSAPrivate:   ecPrivPEM,
		ECDSAPublic:    ecPubPEM,
	}, nil
}

func pemBlock(typ string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

func pemPKCS1Private(key *rsa.PrivateKey) string {
	return pemBlock("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
}

func pemPKCS1Public(key *rsa.PublicKey) string {
	return pemBlock("RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(key))
}

func pemSEC1Private(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("crypto: marshal ecdsa private key: %w", err)
	}

	return pemBlock("EC PRIVATE KEY", der), nil
}

func pemSPKIPublic(key *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", fmt.Errorf("crypto: marshal ecdsa public key: %w", err)
	}

	return pemBlock("PUBLIC KEY", der), nil
}
