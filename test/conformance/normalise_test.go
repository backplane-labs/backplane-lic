package conformance_test

import (
	"strings"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func TestNormaliseLicenseKeys(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("a", 100)
	signature := strings.Repeat("b", 86) + "=="

	for _, key := range []string{
		"key/" + payload + "." + signature,
		payload + "." + signature,
		"eyJhbGciOiJSUzI1NiJ9." + payload + "." + signature,
	} {
		t.Run(key[:20], func(t *testing.T) {
			t.Parallel()

			if got := conformance.Normalise(key, ""); got != "<license-key:1>" {
				t.Fatalf("normalised key = %v", got)
			}
		})
	}
}

func TestNormalisePreservesSemanticStrings(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"1.8", "license.validation.succeeded", "RSA_2048_PKCS1_SIGN_V2", "ALLOW_2X_OVERAGE"} {
		if got := conformance.Normalise(value, ""); got != value {
			t.Errorf("normalised %q = %v", value, got)
		}
	}
}
