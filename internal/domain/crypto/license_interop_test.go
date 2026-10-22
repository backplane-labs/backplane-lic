package crypto_test

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

func TestGeneratedLicenseKeysVerifyWithSDK(t *testing.T) {
	t.Parallel()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const seed = `{"custom":"seed"}`

	for _, test := range []struct {
		scheme licensing.Scheme
		public string
	}{
		{licensing.SchemeEd25519, keys.Ed25519Public},
		{licensing.SchemeECDSAP256, keys.ECDSAPublic},
		{licensing.SchemeRSAPKCS1, keys.RSAPublic},
		{licensing.SchemeRSAPKCS1V2, keys.RSAPublic},
		{licensing.SchemeRSAPSS, keys.RSAPublic},
		{licensing.SchemeRSAPSSV2, keys.RSAPublic},
	} {
		t.Run(string(test.scheme), func(t *testing.T) {
			t.Parallel()

			key, err := crypto.SchemeKey(crypto.KeyInput{
				Keys: keys, Policy: crypto.PolicyKeyFacts{Scheme: string(test.scheme)},
			}, seed)
			if err != nil {
				t.Fatal(err)
			}

			verifier, err := licensing.NewVerifier(test.public)
			if err != nil {
				t.Fatal(err)
			}

			payload, err := verifier.VerifyLicenseKey(test.scheme, key)
			if err != nil || string(payload) != seed {
				t.Fatalf("payload = %q, error = %v", payload, err)
			}
		})
	}
}

func TestGeneratedJWTHeaderMatchesReference(t *testing.T) {
	t.Parallel()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := crypto.SchemeKey(crypto.KeyInput{
		Keys: keys, Policy: crypto.PolicyKeyFacts{Scheme: crypto.SchemeJWT},
	}, `{"sub":"test"}`)
	if err != nil {
		t.Fatal(err)
	}

	header, _, _ := strings.Cut(key, ".")

	raw, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil || string(raw) != `{"alg":"RS256"}` {
		t.Fatalf("header = %q, error = %v", raw, err)
	}
}

func TestLicenseFilesVerifyWithSDK(t *testing.T) {
	t.Parallel()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const payload = `{"data":{"type":"licenses","id":"test-license","attributes":{"key":"test-key"}},"meta":{"issued":"2026-01-01T00:00:00.000Z","expiry":null,"ttl":null}}`

	for _, test := range []struct{ algorithm, public string }{
		{"ed25519", keys.Ed25519Public},
		{"rsa-sha256", keys.RSAPublic},
		{"rsa-pss-sha256", keys.RSAPublic},
		{"ecdsa-p256", keys.ECDSAPublic},
	} {
		for _, encoding := range []string{"base64", "aes-256-gcm"} {
			t.Run(encoding+"+"+test.algorithm, func(t *testing.T) {
				t.Parallel()

				certificate, err := crypto.BuildFileCertificate(crypto.FileCertificateInput{
					Keys: keys, Payload: []byte(payload), Secret: "test-key", RNG: rand.Reader,
					Algorithm: encoding + "+" + test.algorithm,
					Prefix:    "license/", Begin: "-----BEGIN LICENSE FILE-----", End: "-----END LICENSE FILE-----",
				})
				if err != nil {
					t.Fatal(err)
				}

				verifier, err := licensing.NewVerifier(test.public)
				if err != nil {
					t.Fatal(err)
				}

				file := licensing.LicenseFile{Certificate: certificate}
				if err := file.Verify(verifier); err != nil {
					t.Fatal(err)
				}

				var dataset *licensing.LicenseFileDataset
				if encoding == "base64" {
					dataset, err = file.Decode()
				} else {
					dataset, err = file.Decrypt("test-key")
				}

				if err != nil || dataset.License.ID != "test-license" {
					t.Fatalf("dataset = %+v, error = %v", dataset, err)
				}
			})
		}
	}
}
