package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

type sdkTransport struct{ target conformance.Target }

func (s sdkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.target.Do(req)
}

func TestLicenseCheckoutWithSDK(t *testing.T) { //nolint:paralleltest // fixture setup serializes database bootstrap
	target := serverUnderTest(t)
	public := conformance.Ed25519Key(t, target)
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + conformance.AccountSlug
	transcript := conformance.Run(t, target, conformance.Scenario{
		Name: "sdk_checkout",
		Steps: []conformance.Step{
			{
				Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
				Body:    `{"data":{"type":"product","attributes":{"name":"sdk-checkout-{{run}}"}}}`,
				Capture: map[string]string{"product": "data.id"},
			},
			{
				Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
				Body:    `{"data":{"type":"policy","attributes":{"name":"sdk-checkout-{{run}}","scheme":"ED25519_SIGN","strict":false,"floating":true,"authenticationStrategy":"LICENSE"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
				Capture: map[string]string{"policy": "data.id"},
			},
			{
				Name: "license", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
				Body: `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
			},
		},
	}, public)

	raw, err := json.Marshal(transcript.Steps[2].Body)
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Key string `json:"key"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Data.ID == "" {
		t.Fatalf("license fixture: %s: %v", raw, err)
	}

	origin := os.Getenv("LIC_CONFORMANCE_TARGET")
	if origin == "" {
		origin = "http://lic.test"
	}

	client, err := licensing.New(licensing.Config{
		APIURL: origin, LicenseKey: document.Data.Attributes.Key, PublicKey: hex.EncodeToString(public),
		HTTPClient: &http.Client{Transport: sdkTransport{target: target}},
	})
	if err != nil {
		t.Fatal(err)
	}

	license, err := client.Validate(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := license.VerifyKey(); err != nil {
		t.Fatal(err)
	}

	for _, encrypted := range []bool{false, true} {
		file, err := license.Checkout(t.Context(), licensing.CheckoutEncrypt(encrypted))
		if err != nil {
			t.Fatal(err)
		}

		var dataset *licensing.LicenseFileDataset
		if encrypted {
			dataset, err = file.Decrypt(document.Data.Attributes.Key)
		} else {
			dataset, err = file.Decode()
		}

		if err != nil || dataset.License.ID != document.Data.ID {
			t.Fatalf("dataset = %+v, error = %v", dataset, err)
		}
	}
}
