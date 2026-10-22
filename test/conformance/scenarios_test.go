package conformance_test

import (
	"net/http"
	"slices"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// scenarios returns the conformance scenarios. Each must be self-contained:
// it creates what it needs (unique names embed the built-in {{run}} suffix,
// which the normaliser masks) and never depends on global resource counts.
func scenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	basic := conformance.Auth{Kind: conformance.AuthBasic, BasicUser: conformance.AdminEmail, BasicPass: conformance.AdminPass}
	account := "/v1/accounts/" + conformance.AccountSlug
	tokens := account + "/tokens"

	base := []conformance.Scenario{
		{
			Name: "ping",
			Steps: []conformance.Step{
				{Name: "ping", Method: http.MethodGet, Path: "/v1/ping"},
			},
		},
		{
			Name: "health",
			Steps: []conformance.Step{
				{Name: "health", Method: http.MethodGet, Path: "/v1/health"},
			},
		},
		{
			Name: "account_show",
			Steps: []conformance.Step{
				{Name: "show by slug", Method: http.MethodGet, Path: account, Auth: admin},
			},
		},
		{
			Name: "token_generate_basic",
			Steps: []conformance.Step{
				{
					Name:   "generate with password",
					Method: http.MethodPost,
					Path:   tokens,
					Auth:   basic,
					Body:   `{"data":{"type":"tokens","attributes":{"name":"conformance-{{run}}"}}}`,
					Capture: map[string]string{
						"token": "data.attributes.token",
					},
				},
				{
					Name:   "use the generated token",
					Method: http.MethodGet,
					Path:   account,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "token"},
				},
				{
					Name:   "wrong password",
					Method: http.MethodPost,
					Path:   tokens,
					Auth: conformance.Auth{
						Kind:      conformance.AuthBasic,
						BasicUser: conformance.AdminEmail,
						BasicPass: "wrong-password-{{run}}",
					},
					Body: `{"data":{"type":"tokens"}}`,
				},
			},
		},
		{
			Name: "not_found_route",
			Steps: []conformance.Step{
				{Name: "unknown path", Method: http.MethodGet, Path: "/v1/definitely-not-a-route", Auth: admin},
			},
		},
		{
			Name: "bad_accept",
			Steps: []conformance.Step{
				{
					Name:    "html accept",
					Method:  http.MethodGet,
					Path:    account,
					Auth:    admin,
					Headers: map[string]string{"Accept": "text/html"},
				},
			},
		},
		{
			Name: "invalid_json",
			Steps: []conformance.Step{
				{Name: "broken body", Method: http.MethodPost, Path: tokens, Auth: admin, Body: `{"broken`},
			},
		},
		{
			Name: "unauthenticated_account",
			Steps: []conformance.Step{
				{Name: "no credentials", Method: http.MethodGet, Path: account},
			},
		},
	}

	scenarios := slices.Concat(
		base,
		usersScenarios(),
		groupsScenarios(),
		tokensScenarios(),
		productScenarios(),
		policyScenarios(),
		entitlementScenarios(),
		keyScenarios(),
		webhookScenarios(),
		resourceUserScenarios(),
		secondFactorsScenarios(),
		passwordsScenarios(),
		authScenarios(),
	)

	return scenarios
}
