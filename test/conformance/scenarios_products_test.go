package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// productScenarios covers the products group: CRUD, the code alias,
// pagination, the relationship routes and the per-bearer authorization.
func productScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	create := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "create " + name,
			Method: http.MethodPost,
			Path:   account + "/products",
			Auth:   admin,
			Body:   `{"data":{"type":"product","attributes":{"name":"` + name + `-{{run}}"}}}`,
			Capture: map[string]string{
				name + "_id":   "data.id",
				name + "_name": "data.attributes.name",
			},
		}
	}

	show := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "show " + name + " by id",
			Method: http.MethodGet,
			Path:   account + "/products/{{" + name + "_id}}",
			Auth:   admin,
		}
	}

	return []conformance.Scenario{
		{
			Name: "product_crud",
			Steps: []conformance.Step{
				create("crud"),
				show("crud"),
				{
					Name:   "update name url platforms metadata",
					Method: http.MethodPatch,
					Path:   account + "/products/{{crud_id}}",
					Auth:   admin,
					Body: `{"data":{"type":"product","attributes":{"name":"crud-renamed-{{run}}",` +
						`"url":"https://example.com/{{run}}","platforms":["linux","macos"],` +
						`"metadata":{"a_b":{"c_d":1}}}}}`,
				},
				{
					Name:   "update code and alias lookup",
					Method: http.MethodPatch,
					Path:   account + "/products/{{crud_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"code":"crud-{{run}}"}}}`,
				},
				{
					Name:   "show by code",
					Method: http.MethodGet,
					Path:   account + "/products/crud-{{run}}",
					Auth:   admin,
				},
				{
					Name:   "clear url and platforms",
					Method: http.MethodPatch,
					Path:   account + "/products/{{crud_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"url":null,"platforms":null}}}`,
				},
				{
					Name:   "show unknown",
					Method: http.MethodGet,
					Path:   account + "/products/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   account + "/products/{{crud_id}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   account + "/products/{{crud_id}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "product_create_validation",
			Steps: []conformance.Step{
				{
					Name:   "missing attributes",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product"}}`,
				},
				{
					Name:   "blank name",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":""}}}`,
				},
				{
					Name:   "name type mismatch",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":42}}}`,
				},
				{
					Name:   "unpermitted permissions (EE)",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"x","permissions":["a"]}}}`,
				},
				{
					Name:   "unpermitted relationship",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"x"},"relationships":{"policies":{"data":[]}}}}`,
				},
				{
					Name:   "code is a uuid",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"x","code":"90a701a1-1a50-4ecd-b8a4-aeb2d4b04973"}}}`,
				},
				{
					Name:   "url protocol",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"x","url":"ftp://example.com/x"}}}`,
				},
				{
					Name:   "bad type",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"nope","attributes":{"name":"x"}}}`,
				},
				{
					Name:   "create with a code",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"taken-{{run}}","code":"taken-{{run}}"}}}`,
					Capture: map[string]string{
						"taken_id": "data.id",
					},
				},
				{
					Name:   "code taken (case-insensitive)",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{"name":"x2","code":"TAKEN-{{run}}"}}}`,
				},
				{
					Name:   "patch with relationships",
					Method: http.MethodPatch,
					Path:   account + "/products/{{taken_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"product","attributes":{},"relationships":{"policies":{"data":[]}}}}`,
				},
			},
		},
		{
			Name: "product_pagination_params",
			Steps: []conformance.Step{
				create("page1"),
				{
					Name:   "bad limit",
					Method: http.MethodGet,
					Path:   account + "/products?limit=0",
					Auth:   admin,
				},
				{
					Name:   "limit above the maximum",
					Method: http.MethodGet,
					Path:   account + "/products?limit=101",
					Auth:   admin,
				},
				{
					Name:   "bad order",
					Method: http.MethodGet,
					Path:   account + "/products?order=sideways",
					Auth:   admin,
				},
				{
					Name:   "bad cursor",
					Method: http.MethodGet,
					Path:   account + "/products?page[cursor]=notauuid",
					Auth:   admin,
				},
				{
					Name:   "page number above the maximum",
					Method: http.MethodGet,
					Path:   account + "/products?page[number]=101&page[size]=1",
					Auth:   admin,
				},
				{
					Name:   "environment param",
					Method: http.MethodGet,
					Path:   account + "/products?environment=prod",
					Auth:   admin,
				},
				{
					Name:    "environment header",
					Method:  http.MethodGet,
					Path:    account + "/products",
					Auth:    admin,
					Headers: map[string]string{"Keygen-Environment": "prod"},
				},
			},
		},
		{
			Name: "product_relationships",
			Steps: []conformance.Step{
				create("rel"),
				{
					Name:   "create policy under product",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"rel-{{run}}"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{rel_id}}"}}}}}`,
					Capture: map[string]string{
						"rel_policy": "data.id",
					},
				},
				{
					Name:   "product policies index",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/policies",
					Auth:   admin,
				},
				{
					Name:   "product policy show",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/policies/{{rel_policy}}",
					Auth:   admin,
				},
				{
					Name:   "product policy show unknown",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/policies/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "product licenses index",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/licenses",
					Auth:   admin,
				},
				{
					Name:   "product license show unknown",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/licenses/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "product machines index",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/machines",
					Auth:   admin,
				},
				{
					Name:   "product users index",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/users",
					Auth:   admin,
				},
				{
					Name:   "product user show unknown",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/users/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "generate product token",
					Method: http.MethodPost,
					Path:   account + "/products/{{rel_id}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token","attributes":{"name":"rel-{{run}}"}}}`,
					Capture: map[string]string{
						"product_token":       "data.id",
						"product_token_value": "data.attributes.token",
					},
				},
				{
					Name:   "product tokens index",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/tokens",
					Auth:   admin,
				},
				{
					Name:   "product token show",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/tokens/{{product_token}}",
					Auth:   admin,
				},
				{
					Name:   "product token show unknown",
					Method: http.MethodGet,
					Path:   account + "/products/{{rel_id}}/tokens/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
			},
		},
		{
			Name: "product_authz_product_bearer",
			Steps: []conformance.Step{
				create("bearer"),
				create("other"),
				{
					Name:   "generate product token",
					Method: http.MethodPost,
					Path:   account + "/products/{{bearer_id}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token"}}`,
					Capture: map[string]string{
						"ptoken": "data.attributes.token",
					},
				},
				{
					Name:   "show own product",
					Method: http.MethodGet,
					Path:   account + "/products/{{bearer_id}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "show other product",
					Method: http.MethodGet,
					Path:   account + "/products/{{other_id}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "index own only",
					Method: http.MethodGet,
					Path:   account + "/products",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "update own product",
					Method: http.MethodPatch,
					Path:   account + "/products/{{bearer_id}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
					Body:   `{"data":{"type":"product","attributes":{"name":"bearer-renamed-{{run}}"}}}`,
				},
				{
					Name:   "destroy other product",
					Method: http.MethodDelete,
					Path:   account + "/products/{{other_id}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "tokens index denied",
					Method: http.MethodGet,
					Path:   account + "/products/{{bearer_id}}/tokens",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
			},
		},
	}
}
