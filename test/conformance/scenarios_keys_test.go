package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// keyScenarios covers the keys group: the pool members' CRUD,
// validation (including the multi-error document), the filters and the
// product/policy relationships.
func keyScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	product := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "create product " + name,
			Method: http.MethodPost,
			Path:   account + "/products",
			Auth:   admin,
			Body:   `{"data":{"type":"product","attributes":{"name":"` + name + `-{{run}}"}}}`,
			Capture: map[string]string{
				name + "_id": "data.id",
			},
		}
	}

	pooled := func(name, productVar string) conformance.Step {
		return conformance.Step{
			Name:   "create pooled policy " + name,
			Method: http.MethodPost,
			Path:   account + "/policies",
			Auth:   admin,
			Body: `{"data":{"type":"policy","attributes":{"name":"` + name + `-{{run}}","usePool":true},` +
				`"relationships":{"product":{"data":{"type":"product","id":"{{` + productVar + `}}"}}}}}`,
			Capture: map[string]string{
				name + "_id": "data.id",
			},
		}
	}

	policy := func(name, productVar string) conformance.Step {
		return conformance.Step{
			Name:   "create policy " + name,
			Method: http.MethodPost,
			Path:   account + "/policies",
			Auth:   admin,
			Body: `{"data":{"type":"policy","attributes":{"name":"` + name + `-{{run}}"},` +
				`"relationships":{"product":{"data":{"type":"product","id":"{{` + productVar + `}}"}}}}}`,
			Capture: map[string]string{
				name + "_id": "data.id",
			},
		}
	}

	key := func(name, policyVar, value string) conformance.Step {
		return conformance.Step{
			Name:   "create key " + name,
			Method: http.MethodPost,
			Path:   account + "/keys",
			Auth:   admin,
			Body: `{"data":{"type":"key","attributes":{"key":"` + value + `"},` +
				`"relationships":{"policy":{"data":{"type":"policy","id":"{{` + policyVar + `}}"}}}}}`,
			Capture: map[string]string{
				name + "_id": "data.id",
			},
		}
	}

	return []conformance.Scenario{
		{
			Name: "key_crud",
			Steps: []conformance.Step{
				product("kcrud"),
				pooled("kcrudpol", "kcrud_id"),
				key("kcrud", "kcrudpol_id", "b1-key-{{run}}-00000001-aaaaaaaa"),
				{
					Name:   "show",
					Method: http.MethodGet,
					Path:   account + "/keys/{{kcrud_id}}",
					Auth:   admin,
				},
				{
					Name:   "update key value",
					Method: http.MethodPatch,
					Path:   account + "/keys/{{kcrud_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"key","attributes":{"key":"b1-key-{{run}}-00000001-bbbbbbbb"}}}`,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   account + "/keys/{{kcrud_id}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   account + "/keys/{{kcrud_id}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "key_validation",
			Steps: []conformance.Step{
				product("kval"),
				pooled("kvalpool", "kval_id"),
				policy("kvalplain", "kval_id"),
				{
					Name:   "missing relationships",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body:   `{"data":{"type":"key","attributes":{"key":"b1-key-00000002-aaaaaaaa"}}}`,
				},
				{
					Name:   "too short",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body: `{"data":{"type":"key","attributes":{"key":"short"},` +
						`"relationships":{"policy":{"data":{"type":"policy","id":"{{kvalpool_id}}"}}}}}`,
				},
				{
					Name:   "reserved (multi-error document)",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body: `{"data":{"type":"key","attributes":{"key":"actions"},` +
						`"relationships":{"policy":{"data":{"type":"policy","id":"{{kvalpool_id}}"}}}}}`,
				},
				key("kval", "kvalpool_id", "b1-key-{{run}}-00000003-cccccccc"),
				{
					Name:   "taken",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body: `{"data":{"type":"key","attributes":{"key":"b1-key-{{run}}-00000003-cccccccc"},` +
						`"relationships":{"policy":{"data":{"type":"policy","id":"{{kvalpool_id}}"}}}}}`,
				},
				{
					Name:   "unpooled policy",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body: `{"data":{"type":"key","attributes":{"key":"b1-key-{{run}}-00000004-dddddddd"},` +
						`"relationships":{"policy":{"data":{"type":"policy","id":"{{kvalplain_id}}"}}}}}`,
				},
				{
					Name:   "update with relationships",
					Method: http.MethodPatch,
					Path:   account + "/keys/{{kval_id}}",
					Auth:   admin,
					Body: `{"data":{"type":"key","relationships":{"policy":` +
						`{"data":{"type":"policy","id":"{{kvalplain_id}}"}}}}}`,
				},
			},
		},
		{
			Name: "key_index_filters",
			Steps: []conformance.Step{
				product("kfilter"),
				pooled("kpoolfilter", "kfilter_id"),
				key("kfilter1", "kpoolfilter_id", "b1-key-{{run}}-00000005-eeeeeeee"),
				{
					Name:   "index filtered by policy",
					Method: http.MethodGet,
					Path:   account + "/keys?policy={{kpoolfilter_id}}",
					Auth:   admin,
					Capture: map[string]string{
						"kfilter_first": "data.0.id",
					},
				},
				{
					Name:   "index filtered by policy paginated",
					Method: http.MethodGet,
					Path:   account + "/keys?policy={{kpoolfilter_id}}&page[size]=1&page[number]=1",
					Auth:   admin,
				},
				{
					Name:   "index filtered by policy by cursor",
					Method: http.MethodGet,
					Path:   account + "/keys?policy={{kpoolfilter_id}}&page[size]=1&page[cursor]=" + "{{kfilter_first}}",
					Auth:   admin,
				},
				{
					Name:   "index filtered by product",
					Method: http.MethodGet,
					Path:   account + "/keys?product={{kfilter_id}}",
					Auth:   admin,
				},
				{
					Name:   "index filtered by unknown",
					Method: http.MethodGet,
					Path:   account + "/keys?policy=00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "environment param",
					Method: http.MethodGet,
					Path:   account + "/keys?environment=prod",
					Auth:   admin,
				},
			},
		},
		{
			Name: "key_relationships",
			Steps: []conformance.Step{
				product("krel"),
				pooled("krelpol", "krel_id"),
				key("krel", "krelpol_id", "b1-key-{{run}}-00000006-ffffffff"),
				{
					Name:   "key product show",
					Method: http.MethodGet,
					Path:   account + "/keys/{{krel_id}}/product",
					Auth:   admin,
				},
				{
					Name:   "key policy show",
					Method: http.MethodGet,
					Path:   account + "/keys/{{krel_id}}/policy",
					Auth:   admin,
				},
				{
					Name:   "key product show unknown key",
					Method: http.MethodGet,
					Path:   account + "/keys/00000000-0000-0000-0000-000000000000/product",
					Auth:   admin,
				},
			},
		},
	}
}
