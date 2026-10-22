package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// entitlementScenarios covers the entitlements group: CRUD with the
// code alias, validation and pagination.
func entitlementScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	entitlement := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "create entitlement " + name,
			Method: http.MethodPost,
			Path:   account + "/entitlements",
			Auth:   admin,
			Body: `{"data":{"type":"entitlement","attributes":{"name":"` + name + `-{{run}}",` +
				`"code":"` + name + `-{{run}}"}}}`,
			Capture: map[string]string{
				name + "_id":   "data.id",
				name + "_code": "data.attributes.code",
			},
		}
	}

	return []conformance.Scenario{
		{
			Name: "entitlement_crud",
			Steps: []conformance.Step{
				entitlement("crud"),
				{
					Name:   "show by id",
					Method: http.MethodGet,
					Path:   account + "/entitlements/{{crud_id}}",
					Auth:   admin,
				},
				{
					Name:   "show by code",
					Method: http.MethodGet,
					Path:   account + "/entitlements/crud-{{run}}",
					Auth:   admin,
				},
				{
					Name:   "show unknown",
					Method: http.MethodGet,
					Path:   account + "/entitlements/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "update name and metadata",
					Method: http.MethodPatch,
					Path:   account + "/entitlements/{{crud_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"crud-renamed-{{run}}","metadata":{"a_b":1}}}}`,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   account + "/entitlements/{{crud_id}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   account + "/entitlements/{{crud_id}}",
					Auth:   admin,
				},
				{
					Name:   "destroy unknown",
					Method: http.MethodDelete,
					Path:   account + "/entitlements/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
			},
		},
		{
			Name: "entitlement_validation",
			Steps: []conformance.Step{
				{
					Name:   "missing name",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"code":"zz1"}}}`,
				},
				{
					Name:   "missing code",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"zz"}}}`,
				},
				{
					Name:   "blank name",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"","code":"zz2"}}}`,
				},
				{
					Name:   "blank code on update",
					Method: http.MethodPatch,
					Path:   account + "/entitlements/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"code":""}}}`,
				},
				entitlement("taken"),
				{
					Name:   "code taken (case-insensitive)",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"zz","code":"TAKEN-{{run}}"}}}`,
				},
				{
					Name:   "code reserved",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"zz","code":"actions"}}}`,
				},
				{
					Name:   "code is a uuid",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body: `{"data":{"type":"entitlement","attributes":{"name":"zz",` +
						`"code":"90a701a1-1a50-4ecd-b8a4-aeb2d4b04973"}}}`,
				},
				{
					Name:   "unpermitted attribute",
					Method: http.MethodPost,
					Path:   account + "/entitlements",
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"zz","code":"zz3","nope":1}}}`,
				},
			},
		},
		{
			Name: "entitlement_pagination_params",
			Steps: []conformance.Step{
				entitlement("page1"),
				{
					Name:   "bad limit",
					Method: http.MethodGet,
					Path:   account + "/entitlements?limit=0",
					Auth:   admin,
				},
				{
					Name:   "bad order",
					Method: http.MethodGet,
					Path:   account + "/entitlements?order=sideways",
					Auth:   admin,
				},
				{
					Name:   "bad cursor",
					Method: http.MethodGet,
					Path:   account + "/entitlements?page[cursor]=notauuid",
					Auth:   admin,
				},
				{
					Name:   "page number above the maximum",
					Method: http.MethodGet,
					Path:   account + "/entitlements?page[number]=101&page[size]=1",
					Auth:   admin,
				},
				{
					Name:   "environment param",
					Method: http.MethodGet,
					Path:   account + "/entitlements?environment=prod",
					Auth:   admin,
				},
			},
		},
	}
}
