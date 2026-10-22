package conformance_test

import (
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func licenseUserScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + conformance.AccountSlug
	licenses := base + "/licenses"
	owner := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ownerToken"}
	attached := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "attachedToken"}
	outsider := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "outsiderToken"}

	steps := make([]conformance.Step, 0, 90)
	steps = append(steps,
		conformance.Step{
			Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
			Body:    `{"data":{"type":"product","attributes":{"name":"user-licenses-{{run}}"}}}`,
			Capture: map[string]string{"product": "data.id"},
		},
		conformance.Step{
			Name: "unprotected policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"user-licenses-{{run}}","protected":false,"strict":false,"floating":true,"duration":86400,"requireCheckIn":true,"checkInInterval":"day","checkInIntervalCount":1},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"policy": "data.id"},
		})

	for _, name := range []string{"owner", "attached", "outsider"} {
		steps = append(steps, conformance.Step{
			Name: "create " + name, Method: http.MethodPost, Path: base + "/users", Auth: admin,
			Body:    `{"data":{"type":"user","attributes":{"email":"license-` + name + `-{{run}}@example.com"}}}`,
			Capture: map[string]string{name: "data.id"},
		})
	}

	steps = append(steps,
		conformance.Step{
			Name: "owned license", Method: http.MethodPost, Path: licenses, Auth: admin,
			Body:    `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}},"owner":{"data":{"type":"user","id":"{{owner}}"}}}}}`,
			Capture: map[string]string{"license": "data.id", "key": "data.attributes.key"},
		},
		conformance.Step{
			Name: "attach licensee", Method: http.MethodPost, Path: licenses + "/{{license}}/users", Auth: admin,
			Body: `{"data":[{"type":"user","id":"{{attached}}"}]}`,
		},
		conformance.Step{
			Name: "owner me", Method: http.MethodGet, Path: base + "/me", Auth: owner,
			Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
				tb.Helper()

				for _, name := range []string{"owner", "attached", "outsider"} {
					vars[name+"Token"] = conformance.UserToken(tb, target, vars[name])
				}
			},
		},
		conformance.Step{Name: "owner profile", Method: http.MethodGet, Path: base + "/profile", Auth: owner},
	)
	for _, caller := range []struct {
		name string
		auth conformance.Auth
	}{{"owner", owner}, {"attached", attached}, {"outsider", outsider}} {
		steps = append(steps,
			conformance.Step{
				Name: caller.name + " index", Method: http.MethodGet,
				Path: licenses + "?user={{" + caller.name + "}}&limit=100", Auth: caller.auth,
			},
			conformance.Step{
				Name: caller.name + " show", Method: http.MethodGet,
				Path: licenses + "/{{license}}", Auth: caller.auth,
			},
			conformance.Step{
				Name: caller.name + " validate", Method: http.MethodPost,
				Path: licenses + "/{{license}}/actions/validate", Auth: caller.auth, Body: `{}`,
			},
			conformance.Step{
				Name: caller.name + " validate key", Method: http.MethodPost,
				Path: licenses + "/actions/validate-key", Auth: caller.auth, Body: `{"meta":{"key":"{{key}}"}}`,
			},
			conformance.Step{
				Name: caller.name + " update denied", Method: http.MethodPatch,
				Path: licenses + "/{{license}}", Auth: caller.auth,
				Body: `{"data":{"type":"license","attributes":{"name":"renamed"}}}`,
			},
		)
		for _, relationship := range []string{"product", "policy", "owner", "group", "users", "entitlements", "machines", "tokens"} {
			steps = append(steps, conformance.Step{
				Name: caller.name + " " + relationship, Method: http.MethodGet,
				Path: licenses + "/{{license}}/" + relationship, Auth: caller.auth,
			})
		}

		for _, action := range []string{"check-in", "renew", "increment-usage", "decrement-usage", "reset-usage", "suspend", "reinstate"} {
			steps = append(steps, conformance.Step{
				Name: caller.name + " " + action, Method: http.MethodPost,
				Path: licenses + "/{{license}}/actions/" + action, Auth: caller.auth, Body: `{}`,
			})
		}
	}

	steps = append(steps,
		conformance.Step{
			Name: "user creates own license", Method: http.MethodPost, Path: licenses, Auth: owner,
			Body:    `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}},"owner":{"data":{"type":"user","id":"{{owner}}"}}}}}`,
			Capture: map[string]string{"created": "data.id"},
		},
		conformance.Step{
			Name: "protect license override", Method: http.MethodPatch, Path: licenses + "/{{license}}", Auth: admin,
			Body: `{"data":{"type":"license","attributes":{"protected":true}}}`,
		},
		conformance.Step{
			Name: "owner renews protected override", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/renew", Auth: owner, Body: `{}`,
		},
		conformance.Step{
			Name: "owner cannot check in protected override", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-in", Auth: owner, Body: `{}`,
		},
		conformance.Step{
			Name: "owner cannot check out protected override", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-out", Auth: owner, Body: `{}`,
		},
		conformance.Step{
			Name: "attached cannot check out protected override", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-out", Auth: attached, Body: `{}`,
		},
		conformance.Step{
			Name: "unprotect license override", Method: http.MethodPatch, Path: licenses + "/{{license}}", Auth: admin,
			Body: `{"data":{"type":"license","attributes":{"protected":false}}}`,
		},
		conformance.Step{
			Name: "owner check-out", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-out", Auth: owner, Body: `{}`,
		},
		conformance.Step{
			Name: "attached check-out", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-out", Auth: attached, Body: `{}`,
		},
		conformance.Step{
			Name: "outsider check-out", Method: http.MethodPost,
			Path: licenses + "/{{license}}/actions/check-out", Auth: outsider, Body: `{}`,
		},
		conformance.Step{
			Name: "licensee cannot revoke", Method: http.MethodDelete,
			Path: licenses + "/{{license}}/actions/revoke", Auth: attached,
		},
		conformance.Step{
			Name: "owner revokes", Method: http.MethodDelete,
			Path: licenses + "/{{license}}/actions/revoke", Auth: owner,
		},
		conformance.Step{
			Name: "owner destroys", Method: http.MethodDelete,
			Path: licenses + "/{{created}}", Auth: owner,
		},
	)

	return []conformance.Scenario{{Name: "license_user_bearers", Steps: steps}}
}
