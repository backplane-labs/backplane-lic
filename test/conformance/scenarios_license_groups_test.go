package conformance_test

import (
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func licenseGroupOwnerScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	user := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "groupOwnerToken"}
	base := "/v1/accounts/" + conformance.AccountSlug
	steps := []conformance.Step{
		{
			Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
			Body:    `{"data":{"type":"product","attributes":{"name":"group-licenses-{{run}}"}}}`,
			Capture: map[string]string{"product": "data.id"},
		},
		{
			Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"group-licenses-{{run}}"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"policy": "data.id"},
		},
		{
			Name: "membership group", Method: http.MethodPost, Path: base + "/groups", Auth: admin,
			Body:    `{"data":{"type":"groups","attributes":{"name":"member-group-{{run}}"}}}`,
			Capture: map[string]string{"memberGroup": "data.id"},
		},
		{
			Name: "owned group", Method: http.MethodPost, Path: base + "/groups", Auth: admin,
			Body:    `{"data":{"type":"groups","attributes":{"name":"owned-group-{{run}}"}}}`,
			Capture: map[string]string{"ownedGroup": "data.id"},
		},
		{
			Name: "group member", Method: http.MethodPost, Path: base + "/users", Auth: admin,
			Body:    `{"data":{"type":"users","attributes":{"email":"group-license-owner-{{run}}@example.com"},"relationships":{"group":{"data":{"type":"groups","id":"{{memberGroup}}"}}}}}`,
			Capture: map[string]string{"user": "data.id"},
		},
		{
			Name: "license in owned group", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
			Body:    `{"data":{"type":"licenses","relationships":{"policy":{"data":{"type":"policies","id":"{{policy}}"}},"group":{"data":{"type":"groups","id":"{{ownedGroup}}"}}}}}`,
			Capture: map[string]string{"ownedLicense": "data.id"},
		},
		{
			Name: "license in member group", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
			Body:    `{"data":{"type":"licenses","relationships":{"policy":{"data":{"type":"policies","id":"{{policy}}"}},"group":{"data":{"type":"groups","id":"{{memberGroup}}"}}}}}`,
			Capture: map[string]string{"memberLicense": "data.id"},
		},
		{
			Name: "membership alone cannot list licenses", Method: http.MethodGet,
			Path: base + "/groups/{{memberGroup}}/licenses", Auth: user,
			Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
				tb.Helper()

				vars["groupOwnerToken"] = conformance.UserToken(tb, target, vars["user"])
			},
		},
		{
			Name: "attach group owner", Method: http.MethodPost, Path: base + "/groups/{{ownedGroup}}/owners", Auth: admin,
			Body: `{"data":[{"type":"users","id":"{{user}}"}]}`,
		},
		{
			Name: "group owner lists licenses without owning them", Method: http.MethodGet,
			Path: base + "/groups/{{ownedGroup}}/licenses?limit=100", Auth: user,
		},
		{
			Name: "group owner paginates licenses", Method: http.MethodGet,
			Path: base + "/groups/{{ownedGroup}}/licenses?page[number]=1&page[size]=1", Auth: user,
		},
		{
			Name: "group owner shows license", Method: http.MethodGet,
			Path: base + "/groups/{{ownedGroup}}/licenses/{{ownedLicense}}", Auth: user,
		},
		{
			Name: "group owner cannot show same license outside group route", Method: http.MethodGet,
			Path: base + "/licenses/{{ownedLicense}}", Auth: user,
		},
		{
			Name: "group owner sees empty membership group", Method: http.MethodGet,
			Path: base + "/groups/{{memberGroup}}/licenses?limit=100", Auth: user,
		},
		{
			Name: "group owner cannot show membership group license", Method: http.MethodGet,
			Path: base + "/groups/{{memberGroup}}/licenses/{{memberLicense}}", Auth: user,
		},
	}

	return []conformance.Scenario{{Name: "license_group_owner_scope", Steps: steps}}
}
