package crypto_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
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

	rsaPubBlock := pemBlock(t, "PUBLIC KEY", keys.RSAPublic)

	rsaPub, err := x509.ParsePKIXPublicKey(rsaPubBlock)
	if err != nil {
		t.Errorf("rsa public key: %v", err)
	} else if pubKey, ok := rsaPub.(*rsa.PublicKey); !ok || !pubKey.Equal(&rsaKey.PublicKey) {
		t.Error("rsa public key does not match the private key")
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

func TestParseEd25519Seed(t *testing.T) {
	t.Parallel()

	// RFC 8032's first ed25519 test vector.
	const (
		seed   = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
		public = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	)

	tests := []struct {
		name, in string
		wantErr  bool
	}{
		{name: "seed", in: seed},
		{name: "empty", in: "", wantErr: true},
		{name: "not hex", in: "xx" + seed[2:], wantErr: true},
		{name: "short", in: seed[:62], wantErr: true},
		{name: "long", in: seed + "00", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := crypto.ParseEd25519Seed(tc.in)
			if tc.wantErr {
				if !errors.Is(err, crypto.ErrEd25519Seed) || !got.IsZero() {
					t.Fatalf("ParseEd25519Seed(%q) = %v, %v; want ErrEd25519Seed", tc.in, got, err)
				}

				return
			}

			if err != nil || got.Private() != seed || got.Public() != public {
				t.Fatalf("seed %s/%s, err %v", got.Private(), got.Public(), err)
			}
		})
	}
}

func BenchmarkEd25519SeedPublic(b *testing.B) {
	s, err := crypto.ParseEd25519Seed("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		_ = s.Public()
	}
}
