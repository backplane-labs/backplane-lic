package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func licenseEdgeScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + conformance.AccountSlug
	licenses := base + "/licenses"
	steps := make([]conformance.Step, 0, 48)
	steps = append(steps,
		conformance.Step{
			Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
			Body:    `{"data":{"type":"product","attributes":{"name":"license-edges-{{run}}"}}}`,
			Capture: map[string]string{"product": "data.id"},
		},
		conformance.Step{
			Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"license-edges-{{run}}","strict":false,"floating":true},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"policy": "data.id"},
		},
		conformance.Step{
			Name: "license", Method: http.MethodPost, Path: licenses, Auth: admin,
			Body:    `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
			Capture: map[string]string{"license": "data.id", "key": "data.attributes.key"},
		})

	for _, scope := range []string{
		`{"user":null}`, `{"user":""}`, `{"machine":""}`,
		`{"fingerprints":[]}`, `{"fingerprint":""}`, `{"fingerprints":[""]}`,
		`{"components":[]}`, `{"entitlements":[]}`, `{"entitlements":[""]}`,
		`{"product":null}`, `{"policy":null}`, `{"machine":null}`, `{"checksum":null}`, `{"version":null}`,
		`{"fingerprints":null}`, `{"components":null}`, `{"entitlements":null}`,
	} {
		steps = append(steps, conformance.Step{
			Name: "scope " + scope, Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/validate", Auth: admin,
			Body: `{"meta":{"scope":` + scope + `}}`,
		})
	}

	steps = append(steps,
		conformance.Step{
			Name: "require scopes", Method: http.MethodPatch, Path: base + "/policies/{{policy}}", Auth: admin,
			Body: `{"data":{"type":"policy","attributes":{"requireProductScope":true,"requireUserScope":true}}}`,
		},
		conformance.Step{
			Name: "quick ignores scope requirements", Method: http.MethodGet,
			Path: licenses + "/{{license}}/actions/validate", Auth: admin,
		},
		conformance.Step{
			Name: "anonymous missing required scope", Method: http.MethodPost,
			Path: licenses + "/actions/validate-key", Body: `{"meta":{"key":"{{key}}"}}`,
		},
		conformance.Step{
			Name: "anonymous null user matches unowned", Method: http.MethodPost,
			Path: licenses + "/actions/validate-key",
			Body: `{"meta":{"key":"{{key}}","scope":{"product":"{{product}}","user":null}}}`,
		},
	)
	for _, probe := range []struct{ action, meta string }{
		{"increment", `{"increment":-1}`},
		{"decrement", `{"decrement":-2}`},
		{"increment", `{"increment":2147483647}`},
		{"increment", `{"increment":2147483648}`},
		{"increment", `{"increment":9223372036854775807}`},
		{"decrement", `{"decrement":-9223372036854775808}`},
		{"increment", `{"increment":-9223372036854775808}`},
		{"decrement", `{"decrement":9223372036854775807}`},
	} {
		// A fresh counter avoids the reference's asynchronous event callback
		// contending with the next NOWAIT usage update on the same row.
		steps = append(steps, conformance.Step{
			Name: "fresh counter " + probe.meta, Method: http.MethodPost, Path: licenses, Auth: admin,
			Body:    `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
			Capture: map[string]string{"license": "data.id"},
		}, conformance.Step{
			Name: probe.action + " " + probe.meta, Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/" + probe.action + "-usage", Auth: admin,
			Body: `{"meta":` + probe.meta + `}`,
		})
	}

	return []conformance.Scenario{{Name: "license_validation_edges", Steps: steps}}
}
