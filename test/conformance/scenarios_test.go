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
		licensesScenarios(),
		licenseUserScenarios(),
		licenseEdgeScenarios(),
		licenseWebhookScenarios(),
		metadataScenarios(),
		webhookScenarios(),
		notImplementedScenarios(),
	)

	return scenarios
}
