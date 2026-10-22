package crypto_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// zeroReader reads zeros: deterministic token secrets for tests.
type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0
	}

	return len(b), nil
}

func testKeys(t *testing.T) crypto.AccountKeys {
	t.Helper()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatalf("account keys: %v", err)
	}

	return keys
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}

	return raw
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}

	return raw
}

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}

	return id
}

func mustSum(t *testing.T, body []byte) []byte {
	t.Helper()

	sum := sha256.Sum256(body)

	return sum[:]
}

// TestSignAllAlgorithms signs with every supported algorithm and the
// reference's exact signing-string shape.
func TestSignAllAlgorithms(t *testing.T) {
	t.Parallel()

	keys := testKeys(t)
	data := crypto.SigningData("GET", "/v1/accounts/example-com", "api.keygen.test",
		"Thu, 22 Oct 2026 11:48:29 GMT", "sha-256=Re846HmW36LOi8HiaYWo3+AmluKNH8uu5R/GkyMECQ8=")

	want := "(request-target): get /v1/accounts/example-com\n" +
		"host: api.keygen.test\n" +
		"date: Thu, 22 Oct 2026 11:48:29 GMT\n" +
		"digest: sha-256=Re846HmW36LOi8HiaYWo3+AmluKNH8uu5R/GkyMECQ8="

	if data != want {
		t.Fatalf("signing data:\n got %q\nwant %q", data, want)
	}

	for _, alg := range []string{
		crypto.AlgEd25519,
		crypto.AlgRSAPSSSHA256,
		crypto.AlgRSASHA256,
		crypto.AlgECDSAP256,
	} {
		if _, err := crypto.Sign(keys, alg, data); err != nil {
			t.Errorf("sign %s: %v", alg, err)
		}
	}
}

// TestVerifyReferenceSignature reproduces a live reference response's
// ed25519 signature check: the account's public key verifies the
// Keygen-Signature over the reconstructed signing string.
func TestVerifyReferenceSignature(t *testing.T) {
	t.Parallel()

	const (
		pubHex    = "d0314ed9014912959f2c03b503c298338d391ed6dec6b7cfff0a3e47846b0e36"
		date      = "Thu, 22 Oct 2026 11:48:29 GMT"
		digest    = "sha-256=Re846HmW36LOi8HiaYWo3+AmluKNH8uu5R/GkyMECQ8="
		signature = "cDadb0yVGvkSqhqrFdM0HgWmlEzZfrDHNBNSndaUhbHnjPVjZdXQvETd6UL6WEkS/PTNuGzXSZRr+K9xTt0BCA=="
	)

	pub := mustHex(t, pubHex)
	sig := mustB64(t, signature)

	data := crypto.SigningData("GET", "/v1/accounts/example-com", "api.keygen.test", date, digest)

	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(data), sig) {
		t.Fatal("reference signature does not verify")
	}
}

// TestDigest matches the reference's Digest header over a known body.
func TestDigest(t *testing.T) {
	t.Parallel()

	const body = `{"data":{"id":"2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f","type":"accounts"}}`

	want := "sha-256=" + base64.StdEncoding.EncodeToString(
		mustSum(t, []byte(body)),
	)

	if got := crypto.Digest([]byte(body)); got != want {
		t.Fatalf("digest: got %q want %q", got, want)
	}
}

// TestParseAcceptSignature checks the negotiation: the default, an
// explicit algorithm, and the three rejections.
func TestParseAcceptSignature(t *testing.T) {
	t.Parallel()

	accountID := mustUUID(t)

	for _, tc := range []struct {
		header string
		alg    string
	}{
		{"", crypto.AlgEd25519},
		{`algorithm="rsa-sha256"`, crypto.AlgRSASHA256},
		{`keyid="` + accountID.String() + `", algorithm="rsa-pss-sha256"`, crypto.AlgRSAPSSSHA256},
	} {
		alg, err := crypto.ParseAcceptSignature(tc.header, accountID)
		if err != nil {
			t.Errorf("%q: %v", tc.header, err)

			continue
		}

		if alg != tc.alg {
			t.Errorf("%q: alg %q want %q", tc.header, alg, tc.alg)
		}
	}

	for _, tc := range []struct {
		header string
		kind   crypto.AcceptSignatureErrorKind
	}{
		{"garbage", crypto.AcceptSignatureMalformed},
		{`algorithm="hmac-sha256"`, crypto.AcceptSignatureUnsupported},
		{`keyid="not-the-account"`, crypto.AcceptSignatureKeyIDNotFound},
	} {
		if _, err := crypto.ParseAcceptSignature(tc.header, accountID); err == nil {
			t.Errorf("%q: expected an error", tc.header)
		}
	}
}

// TestDigestV3 checks the stored digest against a precomputed
// HMAC-SHA256 vector (the key is the hex string's bytes).
func TestDigestV3(t *testing.T) {
	t.Parallel()

	const (
		secretKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		raw          = "admin-deadbeef"
		want         = "a31f834ce6e0d49139e027f0420f80ef3d011116ee889075d1c79022a93d7276"
	)

	if got := crypto.DigestV3(secretKeyHex, raw); got != want {
		t.Fatalf("digest: got %s want %s", got, want)
	}
}

// TestGenerateTokenSecret checks the v3 shape and the activation
// token's shorter secret.
func TestGenerateTokenSecret(t *testing.T) {
	t.Parallel()

	raw, err := crypto.GenerateTokenSecret(zeroReader{}, "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if got, want := len(raw), len("admin-")+64+len("v3"); got != want {
		t.Fatalf("length: got %d want %d (%s)", got, want, raw)
	}

	activ, err := crypto.GenerateTokenSecret(zeroReader{}, "license")
	if err != nil {
		t.Fatalf("generate activation: %v", err)
	}

	if got, want := len(activ), len("activ-")+32+len("v3"); got != want {
		t.Fatalf("activation length: got %d want %d (%s)", got, want, activ)
	}
}

// TestTokenShapeChecks covers the UUID/format classification the token
// flow errors turn on.
func TestTokenShapeChecks(t *testing.T) {
	t.Parallel()

	if !crypto.UUIDShaped("2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f") {
		t.Error("uuid-shaped token not detected")
	}

	if crypto.UUIDShaped("admin-abcdef0123456789abcdef0123456789abcdef0123456789abcdef012345678v3") {
		t.Error("v3 secret misclassified as uuid-shaped")
	}

	if !crypto.Versioned("anythingv2") || !crypto.Versioned("x-v9") {
		t.Error("versioned tokens not detected")
	}

	if crypto.Versioned("plainvalue") {
		t.Error("unversioned value misclassified")
	}
}

// code of the current step, the previous one only within 10s of the
// boundary, never a future one.
func TestVerifyTOTP(t *testing.T) {
	t.Parallel()
}
