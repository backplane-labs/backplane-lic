package installation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNewKey_HasCorrectLengths(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	if len(key.Seed()) != 32 {
		t.Errorf("seed length = %d, want 32", len(key.Seed()))
	}

	if len(key.Public()) != 32 {
		t.Errorf("public key length = %d, want 32", len(key.Public()))
	}

	if len(key.Fingerprint()) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(key.Fingerprint()))
	}
}

func TestFingerprint_MatchesKey(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fpFromKey := key.Fingerprint()
	fpFromPub := Fingerprint(key.Public())

	if fpFromKey != fpFromPub {
		t.Errorf("Fingerprint mismatch: key=%s pub=%s", fpFromKey, fpFromPub)
	}
}

func TestParseKey_RoundTrip(t *testing.T) {
	t.Parallel()

	key1, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	seed := key1.Seed()

	key2, err := ParseKey(seed)
	if err != nil {
		t.Fatal(err)
	}

	if key2.Fingerprint() != key1.Fingerprint() {
		t.Errorf("fingerprint changed after round-trip")
	}
}

func TestParseKey_RejectsBadSeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed []byte
	}{
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
		{"empty", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseKey(tc.seed)
			if err == nil {
				t.Error("expected error for bad seed")
			}
		})
	}
}

func TestSign_Verifies(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("hello, world")
	sig := key.Sign(msg)

	if !ed25519.Verify(key.Public(), msg, sig) {
		t.Error("signature did not verify against own public key")
	}
}

func TestFingerprint_KnownVector(t *testing.T) {
	t.Parallel()

	pub := make([]byte, 32)
	hash := sha256.Sum256(pub)
	expected := hex.EncodeToString(hash[:])

	got := Fingerprint(pub)

	if got != expected {
		t.Errorf("Fingerprint = %s, want %s", got, expected)
	}
}
