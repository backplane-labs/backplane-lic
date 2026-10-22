package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// groupsScenarios covers the groups resource group: CRUD, owners and
// the member relationship indexes.
func groupsScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	return []conformance.Scenario{
		{
			Name: "groups_crud",
			Steps: []conformance.Step{
				{
					Name:   "create",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body: `{"data":{"type":"groups","attributes":{` +
						`"name":"gcrud-{{run}}","maxUsers":5,"maxLicenses":null,` +
						`"metadata":{"costCenter":"cc-1"}}}}`,
					Capture: map[string]string{"groupId": "data.id"},
				},
				{
					Name:   "show",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "patch",
					Method: http.MethodPatch,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
					Body: `{"data":{"type":"groups","attributes":` +
						`{"maxUsers":null,"maxMachines":3,"metadata":{"costCenter":"cc-2"}}}}`,
				},
				{
					Name:   "delete",
					Method: http.MethodDelete,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "show after delete",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "unauthenticated create",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"data":{"type":"groups","attributes":{"name":"x-{{run}}"}}}`,
				},
			},
		},
		{
			Name: "groups_validation",
			Steps: []conformance.Step{
				{
					Name:   "missing name",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","attributes":{"maxUsers":1}}}`,
				},
				{
					Name:   "blank name",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","attributes":{"name":""}}}`,
				},
				{
					Name:   "bad type",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"name":"x-{{run}}"}}}`,
				},
				{
					Name:   "unpermitted attribute",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","attributes":{"name":"x-{{run}}","bogus":true}}}`,
				},
				{
					Name:   "wrong-typed limit",
					Method: http.MethodPost,
					Path:   account + "/groups",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","attributes":{"name":"x-{{run}}","maxUsers":"many"}}}`,
				},
				{
					Name:   "unknown group show",
					Method: http.MethodGet,
					Path:   account + "/groups/00000000-0000-0000-0000-000000000002",
					Auth:   admin,
				},
			},
		},
		{
			Name: "group_owners",
			Steps: []conformance.Step{
				{
					Name:    "create group",
					Method:  http.MethodPost,
					Path:    account + "/groups",
					Auth:    admin,
					Body:    `{"data":{"type":"groups","attributes":{"name":"own-{{run}}"}}}`,
					Capture: map[string]string{"groupId": "data.id"},
				},
				{
					Name:   "create owner user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"owner-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"ownerId": "data.id"},
				},
				{
					Name:   "owners empty",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
				},
				{
					Name:    "attach owner",
					Method:  http.MethodPost,
					Path:    account + "/groups/{{groupId}}/owners",
					Auth:    admin,
					Body:    `{"data":[{"type":"users","id":"{{ownerId}}"}]}`,
					Capture: map[string]string{"joinId": "data.0.id"},
				},
				{
					Name:   "attach duplicate",
					Method: http.MethodPost,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
					Body:   `{"data":[{"type":"users","id":"{{ownerId}}"}]}`,
				},
				{
					Name:   "attach unknown user",
					Method: http.MethodPost,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
					Body:   `{"data":[{"type":"users","id":"00000000-0000-0000-0000-000000000003"}]}`,
				},
				{
					Name:   "owners index",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/owners?limit=100",
					Auth:   admin,
				},
				{
					Name:   "owner show",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/owners/{{joinId}}",
					Auth:   admin,
				},
				{
					Name:   "owner show unknown",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/owners/00000000-0000-0000-0000-000000000004",
					Auth:   admin,
				},
				{
					Name:   "detach unknown owner",
					Method: http.MethodDelete,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
					Body:   `{"data":[{"type":"users","id":"00000000-0000-0000-0000-000000000005"}]}`,
				},
				{
					Name:   "detach owner",
					Method: http.MethodDelete,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
					Body:   `{"data":[{"type":"users","id":"{{ownerId}}"}]}`,
				},
				{
					Name:   "owners empty again",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/owners",
					Auth:   admin,
				},
			},
		},
		{
			Name: "group_relationships_empty",
			Steps: []conformance.Step{
				{
					Name:    "create group",
					Method:  http.MethodPost,
					Path:    account + "/groups",
					Auth:    admin,
					Body:    `{"data":{"type":"groups","attributes":{"name":"rel-{{run}}"}}}`,
					Capture: map[string]string{"groupId": "data.id"},
				},
				{
					Name:   "licenses empty",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/licenses",
					Auth:   admin,
				},
				{
					Name:   "license show unknown",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/licenses/not-a-key",
					Auth:   admin,
				},
				{
					Name:   "machines empty",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/machines",
					Auth:   admin,
				},
				{
					Name:   "machine show unknown",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/machines/not-a-fingerprint",
					Auth:   admin,
				},
			},
		},
	}
}
