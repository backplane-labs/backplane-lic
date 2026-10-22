package licensing_test

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	licensing "github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// testAccount holds a generated account key set, mirroring Keygen's
// per-account keys.
type testAccount struct {
	ed25519Priv ed25519.PrivateKey
	rsaPriv     *rsa.PrivateKey
	ecdsaPriv   *ecdsa.PrivateKey
}

func newTestAccount(t *testing.T) testAccount {
	t.Helper()

	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519: %v", err)
	}

	if len(edPub) != ed25519.PublicKeySize {
		t.Fatal("unexpected ed25519 key size")
	}

	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa: %v", err)
	}

	ecdsaPriv, err := ecdsa.GenerateKey(ellipticP256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa: %v", err)
	}

	return testAccount{ed25519Priv: edPriv, rsaPriv: rsaPriv, ecdsaPriv: ecdsaPriv}
}

func (a testAccount) ed25519Hex() string {
	return fmt.Sprintf("%x", a.ed25519Priv.Public().(ed25519.PublicKey))
}

func (a testAccount) rsaPEM(t *testing.T) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&a.rsaPriv.PublicKey)
	if err != nil {
		t.Fatalf("marshal rsa: %v", err)
	}

	return pemBlock(t, "PUBLIC KEY", der)
}

func (a testAccount) ecdsaPEM(t *testing.T) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&a.ecdsaPriv.PublicKey)
	if err != nil {
		t.Fatalf("marshal ecdsa: %v", err)
	}

	return pemBlock(t, "PUBLIC KEY", der)
}

// sign produces a Keygen wire signature with the account key matching
// the algorithm.
func (a testAccount) sign(t *testing.T, algorithm string, msg []byte) []byte {
	t.Helper()

	switch algorithm {
	case "ed25519":
		return ed25519.Sign(a.ed25519Priv, msg)
	case "rsa-sha256":
		digest := sha256.Sum256(msg)

		sig, err := rsa.SignPKCS1v15(rand.Reader, a.rsaPriv, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("sign rsa: %v", err)
		}

		return sig
	case "rsa-pss-sha256":
		digest := sha256.Sum256(msg)

		sig, err := rsa.SignPSS(rand.Reader, a.rsaPriv, crypto.SHA256, digest[:], nil)
		if err != nil {
			t.Fatalf("sign rsa-pss: %v", err)
		}

		return sig
	case "ecdsa-p256":
		digest := sha256.Sum256(msg)

		sig, err := ecdsa.SignASN1(rand.Reader, a.ecdsaPriv, digest[:])
		if err != nil {
			t.Fatalf("sign ecdsa: %v", err)
		}

		return sig
	default:
		t.Fatalf("unknown algorithm %q", algorithm)

		return nil
	}
}

// certificateDoc is the {enc, sig, alg} document inside a file block.
type certificateDoc struct {
	Enc string `json:"enc"`
	Sig string `json:"sig"`
	Alg string `json:"alg"`
}

// buildCertificate constructs a Keygen-format license/machine file
// certificate: encode (optionally encrypt) the payload, sign prefix+enc,
// wrap the JSON document in the PEM-like block.
func buildCertificate(
	t *testing.T,
	account testAccount,
	prefix, begin, end string,
	payload []byte,
	encrypt bool,
	secret string,
	signAlgorithm string,
) string {
	t.Helper()

	enc := base64.StdEncoding.EncodeToString(payload)

	if encrypt {
		key := sha256.Sum256([]byte(secret))

		block, err := aes.NewCipher(key[:])
		if err != nil {
			t.Fatalf("aes: %v", err)
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatalf("gcm: %v", err)
		}

		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			t.Fatalf("nonce: %v", err)
		}

		sealed := gcm.Seal(nil, nonce, payload, nil)

		ciphertext, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]
		enc = strings.Join([]string{
			base64.StdEncoding.EncodeToString(ciphertext),
			base64.StdEncoding.EncodeToString(nonce),
			base64.StdEncoding.EncodeToString(tag),
		}, ".")
	}

	encoding := "base64"
	if encrypt {
		encoding = "aes-256-gcm"
	}

	doc := certificateDoc{
		Enc: enc,
		Sig: base64.StdEncoding.EncodeToString(account.sign(t, signAlgorithm, []byte(prefix+enc))),
		Alg: encoding + "+" + signAlgorithm,
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal certificate: %v", err)
	}

	// Line-wrap the base64 like the reference does.
	encoded := base64.StdEncoding.EncodeToString(raw)

	var wrapped strings.Builder

	for len(encoded) > 60 {
		wrapped.WriteString(encoded[:60] + "\n")

		encoded = encoded[60:]
	}

	wrapped.WriteString(encoded)

	return begin + "\n" + wrapped.String() + "\n" + end
}

// licensePayload renders an inner license-file JSON:API payload.
func licensePayload(t *testing.T, issued time.Time, ttl int, entitlementCodes []string) []byte {
	t.Helper()

	expiry := "null"
	if ttl != 0 {
		expiry = `"` + issued.Add(time.Duration(ttl)*time.Second).UTC().Format(time.RFC3339Nano) + `"`
	}

	var included strings.Builder

	for _, code := range entitlementCodes {
		included.WriteString(`{"id":"ent-` + code + `","type":"entitlements","attributes":{"code":"` + code + `"}},`)
	}

	payload := `{"data":{"id":"lic-1","type":"licenses","attributes":{"name":"n","key":"k","scheme":"ED25519_SIGN","metadata":{"fooBar":"baz"}},"relationships":{"policy":{"data":{"type":"policies","id":"pol-1"}}}},"meta":{"issued":"` +
		issued.UTC().Format(time.RFC3339Nano) + `","expiry":` + expiry + `,"ttl":` + strconv.Itoa(ttl) + `}`

	if included.Len() > 0 {
		payload += `,"included":[` + strings.TrimSuffix(included.String(), ",") + `]`
	}

	payload += `}`

	return []byte(payload)
}

func TestLicenseFileVerifyDecrypt(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	const secret = "key/abc.def"

	certificateBlock := buildCertificate(t, account, "license/",
		"-----BEGIN LICENSE FILE-----", "-----END LICENSE FILE-----",
		licensePayload(t, time.Now(), 3600, []string{"premium"}), true, secret, "ed25519")

	file, err := licensing.ParseLicenseFile(certificateBlock)
	if err != nil {
		t.Fatalf("ParseLicenseFile: %v", err)
	}

	if file.Algorithm != "aes-256-gcm+ed25519" {
		t.Fatalf("algorithm = %q", file.Algorithm)
	}

	if err := file.Verify(verifier); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	dataset, err := file.Decrypt(secret)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if dataset.License.ID != "lic-1" || dataset.License.Name != "n" {
		t.Fatalf("license = %+v", dataset.License)
	}

	if dataset.License.PolicyID != "pol-1" {
		t.Fatalf("policy id = %q", dataset.License.PolicyID)
	}

	if len(dataset.Entitlements) != 1 || dataset.Entitlements[0].Code != "premium" {
		t.Fatalf("entitlements = %+v", dataset.Entitlements)
	}

	if value, ok := dataset.License.Metadata.String("fooBar"); !ok || value != "baz" {
		t.Fatalf("metadata = %v %v", value, ok)
	}

	if dataset.TTL != 3600 {
		t.Fatalf("ttl = %d", dataset.TTL)
	}

	// Wrong key and tampered certificates must not verify or decrypt.
	if _, err := file.Decrypt("wrong-secret"); !errors.Is(err, licensing.ErrCertificateInvalid) {
		t.Fatalf("wrong secret err = %v", err)
	}

	tampered := strings.Replace(certificateBlock, "LICENSE FILE", "MACHINE FILE", 1)
	if _, err := licensing.ParseMachineFile(tampered); err == nil {
		t.Fatal("machine-file parse of a license file should fail")
	}
}

func TestLicenseFileDecodeUnencrypted(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	block := buildCertificate(t, account, "license/",
		"-----BEGIN LICENSE FILE-----", "-----END LICENSE FILE-----",
		licensePayload(t, time.Now(), 0, nil), false, "", "ed25519")

	file, err := licensing.ParseLicenseFile(block)
	if err != nil {
		t.Fatalf("ParseLicenseFile: %v", err)
	}

	if err := file.Verify(verifier); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, err := file.Decrypt("secret"); !errors.Is(err, licensing.ErrFileNotEncrypted) {
		t.Fatalf("Decrypt err = %v", err)
	}

	dataset, err := file.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if dataset.License.ID != "lic-1" {
		t.Fatalf("license id = %q", dataset.License.ID)
	}
}

func TestLicenseFileExpired(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	block := buildCertificate(t, account, "license/",
		"-----BEGIN LICENSE FILE-----", "-----END LICENSE FILE-----",
		licensePayload(t, time.Now().Add(-2*time.Hour), 3600, nil), false, "", "ed25519")

	file, err := licensing.ParseLicenseFile(block)
	if err != nil {
		t.Fatalf("ParseLicenseFile: %v", err)
	}

	if _, err := file.Decode(); !errors.Is(err, licensing.ErrLicenseFileExpired) {
		t.Fatalf("Decode err = %v", err)
	}
}

func TestLicenseFileClockSkew(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	// Issued 10 minutes in the future: beyond the 5m skew guard.
	block := buildCertificate(t, account, "license/",
		"-----BEGIN LICENSE FILE-----", "-----END LICENSE FILE-----",
		licensePayload(t, time.Now().Add(10*time.Minute), 3600, nil), false, "", "ed25519")

	file, err := licensing.ParseLicenseFile(block)
	if err != nil {
		t.Fatalf("ParseLicenseFile: %v", err)
	}

	if _, err := file.Decode(); !errors.Is(err, licensing.ErrClockOutOfSync) {
		t.Fatalf("Decode err = %v", err)
	}
}

func TestMachineFileVerifyDecrypt(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	const licenseKey = "key/xyz"

	const fingerprint = "fp-1"

	machinePayload := `{"data":{"id":"mach-1","type":"machines","attributes":{"fingerprint":"` +
		fingerprint + `","hostname":"h"}},"included":[{"id":"lic-1","type":"licenses","attributes":{"name":"n"}},` +
		`{"id":"comp-1","type":"components","attributes":{"fingerprint":"cfp-1","name":"gpu"}}],` +
		`"meta":{"issued":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","expiry":null,"ttl":0}}`

	block := buildCertificate(t, account, "machine/",
		"-----BEGIN MACHINE FILE-----", "-----END MACHINE FILE-----",
		[]byte(machinePayload), true, licenseKey+fingerprint, "ed25519")

	file, err := licensing.ParseMachineFile(block)
	if err != nil {
		t.Fatalf("ParseMachineFile: %v", err)
	}

	if err := file.Verify(verifier); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	dataset, err := file.Decrypt(licenseKey, fingerprint)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if dataset.Machine.ID != "mach-1" || dataset.Machine.Fingerprint != fingerprint {
		t.Fatalf("machine = %+v", dataset.Machine)
	}

	if dataset.License.ID != "lic-1" {
		t.Fatalf("license id = %q", dataset.License.ID)
	}

	if len(dataset.Components) != 1 || dataset.Components[0].Fingerprint != "cfp-1" {
		t.Fatalf("components = %+v", dataset.Components)
	}

	if _, err := file.Decrypt(licenseKey, "wrong-fp"); !errors.Is(err, licensing.ErrCertificateInvalid) {
		t.Fatalf("wrong fingerprint err = %v", err)
	}
}

func TestFileSignatureAlgorithms(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	for _, algorithm := range []string{"rsa-sha256", "rsa-pss-sha256", "ecdsa-p256"} {
		t.Run(algorithm, func(t *testing.T) {
			t.Parallel()

			var key string

			switch algorithm {
			case "rsa-sha256", "rsa-pss-sha256":
				key = account.rsaPEM(t)
			case "ecdsa-p256":
				key = account.ecdsaPEM(t)
			}

			verifier, err := licensing.NewVerifier(key)
			if err != nil {
				t.Fatalf("NewVerifier: %v", err)
			}

			block := buildCertificate(t, account, "license/",
				"-----BEGIN LICENSE FILE-----", "-----END LICENSE FILE-----",
				licensePayload(t, time.Now(), 0, nil), false, "", algorithm)

			file, err := licensing.ParseLicenseFile(block)
			if err != nil {
				t.Fatalf("ParseLicenseFile: %v", err)
			}

			if err := file.Verify(verifier); err != nil {
				t.Fatalf("Verify: %v", err)
			}

			if _, err := file.Decode(); err != nil {
				t.Fatalf("Decode: %v", err)
			}
		})
	}
}

func TestVerifyLicenseKey(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	dataset := []byte(`{"account":{"id":"acc-1"},"license":{"id":"lic-1"}}`)

	encDataset := base64.URLEncoding.EncodeToString(dataset)

	msg := []byte("key/" + encDataset)

	key := "key/" + encDataset + "." + base64.URLEncoding.EncodeToString(account.sign(t, "ed25519", msg))

	got, err := verifier.VerifyLicenseKey(licensing.SchemeEd25519, key)
	if err != nil {
		t.Fatalf("VerifyLicenseKey: %v", err)
	}

	if !bytes.Equal(got, dataset) {
		t.Fatalf("dataset = %s", got)
	}

	// A forged signature is rejected.
	forged := "key/" + encDataset + "." + base64.URLEncoding.EncodeToString([]byte("garbage"))
	if _, err := verifier.VerifyLicenseKey(licensing.SchemeEd25519, forged); !errors.Is(err, licensing.ErrLicenseKeyNotGenuine) {
		t.Fatalf("forged err = %v", err)
	}
}

func TestVerifyLicenseKeyRSASchemes(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.rsaPEM(t))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	dataset := []byte(`{"id":"lic-1"}`)

	encDataset := base64.URLEncoding.EncodeToString(dataset)

	cases := []struct {
		scheme licensing.Scheme
		prefix bool
	}{
		{licensing.SchemeRSAPKCS1V2, true},
		{licensing.SchemeRSAPSSV2, true},
		{licensing.SchemeRSAPKCS1, false},
		{licensing.SchemeRSAPSS, false},
	}

	for _, testCase := range cases {
		t.Run(string(testCase.scheme), func(t *testing.T) {
			t.Parallel()

			msg := dataset
			if testCase.prefix {
				msg = []byte("key/" + encDataset)
			}

			algorithm := "rsa-sha256"
			if testCase.scheme == licensing.SchemeRSAPSSV2 || testCase.scheme == licensing.SchemeRSAPSS {
				algorithm = "rsa-pss-sha256"
			}

			key := encDataset + "." + base64.URLEncoding.EncodeToString(account.sign(t, algorithm, msg))
			if testCase.prefix {
				key = "key/" + key
			}

			got, err := verifier.VerifyLicenseKey(testCase.scheme, key)
			if err != nil {
				t.Fatalf("VerifyLicenseKey: %v", err)
			}

			if !bytes.Equal(got, dataset) {
				t.Fatalf("dataset = %s", got)
			}
		})
	}
}

// signedExchange is a signed HTTP request/response pair.
func signedExchange(
	t *testing.T,
	account testAccount,
	method, target, host string,
	body []byte,
) (*http.Request, http.Header) {
	t.Helper()

	request, err := http.NewRequest(method, "http://irrelevant"+target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	request.Host = host

	shasum := sha256.Sum256(body)
	digest := "sha-256=" + base64.StdEncoding.EncodeToString(shasum[:])

	date := time.Now().UTC().Format(http.TimeFormat)

	signingData := fmt.Sprintf(
		"(request-target): %s %s\nhost: %s\ndate: %s\ndigest: %s",
		strings.ToLower(method), target, host, date, digest,
	)

	header := http.Header{}
	header.Set("Digest", digest)
	header.Set("Date", date)
	header.Set("Keygen-Signature", fmt.Sprintf(
		`keyid="acc-1", algorithm="ed25519", signature=%q, headers="(request-target) host date digest"`,
		base64.StdEncoding.EncodeToString(account.sign(t, "ed25519", []byte(signingData))),
	))

	return request, header
}

func TestVerifyResponseSignature(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	body := []byte(`{"data":{"id":"lic-1"}}`)
	request, header := signedExchange(t, account, "POST", "/v1/licenses/lic-1/actions/validate?x=1", "api.keygen.test", body)

	if err := verifier.VerifyResponse(request, header, body, licensing.SkewGuardFor(time.Minute)); err != nil {
		t.Fatalf("VerifyResponse: %v", err)
	}

	// A tampered body fails the digest.
	if err := verifier.VerifyResponse(request, header, []byte("tampered"), licensing.SkewGuardFor(time.Minute)); !errors.Is(err, licensing.ErrDigestInvalid) {
		t.Fatalf("tampered err = %v", err)
	}

	// A stale Date fails the skew guard.
	header.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))

	if err := verifier.VerifyResponse(request, header, body, licensing.SkewGuardFor(time.Minute)); !errors.Is(err, licensing.ErrDateTooOld) {
		t.Fatalf("stale date err = %v", err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	t.Parallel()

	account := newTestAccount(t)

	verifier, err := licensing.NewVerifier(account.ed25519Hex())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	body := []byte(`{"data":{"type":"events","id":"evt-1"}}`)
	_, header := signedExchange(t, account, "POST", "/webhooks", "hooks.example.com", body)

	request, err := http.NewRequest(http.MethodPost, "http://hooks.example.com/webhooks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	request.Host = "hooks.example.com"
	request.Header = header

	if err := verifier.VerifyWebhook(request, licensing.SkewGuardFor(time.Minute)); err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}

	// The body remains readable after verification.
	readBack, err := readAll(request)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if !bytes.Equal(readBack, body) {
		t.Fatalf("body = %s", readBack)
	}

	// A tampered signature is rejected.
	request.Header.Set("Keygen-Signature", `keyid="x", algorithm="ed25519", signature="Zm9v", headers="(request-target)"`)

	if err := verifier.VerifyWebhook(request, licensing.SkewGuardFor(time.Minute)); err == nil {
		t.Fatal("tampered webhook verified")
	}
}
