package conformance_test

import (
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func licenseWebhookScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + conformance.AccountSlug
	endpoint := "https://example.com/hook?license-validation={{run}}"
	steps := make([]conformance.Step, 0, 9)
	steps = append(steps,
		conformance.Step{
			Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
			Body:    `{"data":{"type":"product","attributes":{"name":"license-webhook-{{run}}"}}}`,
			Capture: map[string]string{"product": "data.id"},
		},
		conformance.Step{
			Name: "endpoint", Method: http.MethodPost, Path: base + "/webhook-endpoints", Auth: admin,
			Body:    `{"data":{"type":"webhook-endpoints","attributes":{"url":"` + endpoint + `","subscriptions":["license.validation.succeeded","license.validation.failed"]},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"endpoint": "data.id"},
		},
		conformance.Step{
			Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"license-webhook-{{run}}","strict":false,"floating":true},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"policy": "data.id"},
		},
		conformance.Step{
			Name: "license", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
			Body:    `{"data":{"type":"license","attributes":{"name":"license-webhook-{{run}}"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
			Capture: map[string]string{"license": "data.id", "key": "data.attributes.key"},
		},
		conformance.Step{
			Name: "anonymous validation succeeds", Method: http.MethodPost, Path: base + "/licenses/actions/validate-key",
			Body: `{"meta":{"key":"{{key}}","nonce":42,"scope":{"product":"{{product}}","user":null}}}`,
		}, licenseValidationEventStep(base, endpoint, "succeeded"), conformance.Step{
			Name: "validation fails", Method: http.MethodPost, Path: base + "/licenses/{{license}}/actions/validate", Auth: admin,
			Body: `{"meta":{"nonce":0,"scope":{"product":"other-product"}}}`,
		}, licenseValidationEventStep(base, endpoint, "failed"), conformance.Step{
			Name: "remove endpoint", Method: http.MethodDelete, Path: base + "/webhook-endpoints/{{endpoint}}", Auth: admin,
		})

	return []conformance.Scenario{{Name: "license_validation_webhooks", Steps: steps}}
}

func licenseValidationEventStep(base, endpoint, outcome string) conformance.Step {
	return conformance.Step{
		Name: "show validation " + outcome + " event", Method: http.MethodGet,
		Path: base + "/webhook-events/{{event}}", Auth: conformance.Auth{Kind: conformance.AuthAdmin},
		Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
			tb.Helper()

			conformance.WaitForEvent(tb, target, vars, "event", endpoint,
				"license.validation."+outcome, "license-webhook-{{run}}")
		},
	}
}
