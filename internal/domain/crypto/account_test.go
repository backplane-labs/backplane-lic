package crypto_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

func TestGenerateAccountKeys(t *testing.T) {
	t.Parallel()

	// Deterministic input: the keys only need to be well-formed, not
	// unpredictable, for the format assertions.
	keys, err := crypto.GenerateAccountKeys(bytes.NewReader(make([]byte, 1<<20)))
	if err != nil {
		t.Fatalf("GenerateAccountKeys: %v", err)
	}

	if got := len(keys.SecretKey); got != 128 {
		t.Errorf("secret key: %d hex chars, want 128", got)
	}

	if _, err := hex.DecodeString(keys.SecretKey); err != nil {
		t.Errorf("secret key: not hex: %v", err)
	}

	rsaBlock := pemBlock(t, "RSA PRIVATE KEY", keys.RSAPrivate)

	rsaKey, err := x509.ParsePKCS1PrivateKey(rsaBlock)
	if err != nil {
		t.Fatalf("rsa private key: %v", err)
	}

	if rsaKey.N.BitLen() != 2048 {
		t.Errorf("rsa key: %d bits, want 2048", rsaKey.N.BitLen())
	}

	rsaPubBlock := pemBlock(t, "RSA PUBLIC KEY", keys.RSAPublic)
	if _, err := x509.ParsePKCS1PublicKey(rsaPubBlock); err != nil {
		t.Errorf("rsa public key: %v", err)
	}

	seed, err := hex.DecodeString(keys.Ed25519Private)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Errorf("ed25519 private: hex %d bytes, err %v", len(seed), err)
	}

	pub, err := hex.DecodeString(keys.Ed25519Public)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Errorf("ed25519 public: hex %d bytes, err %v", len(pub), err)
	}

	if !ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(pub)) {
		t.Error("ed25519: public key does not match the private seed")
	}

	ecBlock := pemBlock(t, "EC PRIVATE KEY", keys.ECDSAPrivate)

	ecKey, err := x509.ParseECPrivateKey(ecBlock)
	if err != nil {
		t.Fatalf("ecdsa private key: %v", err)
	}

	if ecKey.Curve != elliptic.P256() {
		t.Errorf("ecdsa curve: %v, want P-256", ecKey.Curve)
	}

	ecPubBlock := pemBlock(t, "PUBLIC KEY", keys.ECDSAPublic)

	ecPub, err := x509.ParsePKIXPublicKey(ecPubBlock)
	if err != nil {
		t.Fatalf("ecdsa public key: %v", err)
	}

	ecPubKey, ok := ecPub.(*ecdsa.PublicKey)
	if !ok || !ecPubKey.Equal(&ecKey.PublicKey) {
		t.Error("ecdsa: public key does not match the private key")
	}
}

func pemBlock(t *testing.T, typ, pemStr string) []byte {
	t.Helper()

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatalf("%s: not PEM", typ)
	}

	if block.Type != typ {
		t.Fatalf("%s: got block type %q", typ, block.Type)
	}

	return block.Bytes
}
