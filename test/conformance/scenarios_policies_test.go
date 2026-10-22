package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// policyScenarios covers the policies group: CRUD over the full
// attribute surface, the immutable attributes, the product/license
// relationship routes, the key pool and entitlement attach/detach.
func policyScenarios() []conformance.Scenario {
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
			Name: "policy_crud",
			Steps: []conformance.Step{
				product("pcrud"),
				policy("pcrudpol", "pcrud_id"),
				{
					Name:   "show",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pcrudpol_id}}",
					Auth:   admin,
				},
				{
					Name:   "update name and clear max machines",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pcrudpol_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"pcrud-renamed-{{run}}","maxMachines":null}}}`,
				},
				{
					Name:   "update protected and strict",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pcrudpol_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"protected":false,"strict":true}}}`,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{pcrudpol_id}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pcrudpol_id}}",
					Auth:   admin,
				},
				{
					Name:   "destroy unknown",
					Method: http.MethodDelete,
					Path:   account + "/policies/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
			},
		},
		{
			Name: "policy_create_validation",
			Steps: []conformance.Step{
				product("pval"),
				{
					Name:   "missing relationships",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"x"}}}`,
				},
				{
					Name:   "blank name",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":""},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "unknown product",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"00000000-0000-0000-0000-000000000000"}}}}}`,
				},
				{
					Name:   "duration below one day",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","duration":100},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "unknown scheme",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","scheme":"NOPE"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "node-locked with three machines",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","maxMachines":3},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "floating with zero machines",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","floating":true,"maxMachines":0},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "legacy concurrent alias",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","concurrent":true},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "legacy leasing alias",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","leasingStrategy":"PER_LICENSE"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
				{
					Name:   "unpermitted attribute",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x","nope":1},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pval_id}}"}}}}}`,
				},
			},
		},
		{
			Name: "policy_update_immutable",
			Steps: []conformance.Step{
				product("pimm"),
				policy("pimmpol", "pimm_id"),
				{
					Name:   "patch scheme",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pimmpol_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"scheme":"ED25519_SIGN"}}}`,
				},
				{
					Name:   "patch encrypted",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pimmpol_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"encrypted":true}}}`,
				},
				{
					Name:   "patch usePool",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pimmpol_id}}",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"usePool":true}}}`,
				},
				{
					Name:   "patch relationships",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pimmpol_id}}",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"x"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pimm_id}}"}}}}}`,
				},
			},
		},
		{
			Name: "policy_full_attributes",
			Steps: []conformance.Step{
				product("pfull"),
				{
					Name:   "create with every attribute",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   admin,
					Body: `{"data":{"type":"policy","attributes":{"name":"pfull-{{run}}","duration":2592000,` +
						`"strict":true,"floating":true,"maxMachines":5,"maxProcesses":10,"maxUsers":20,` +
						`"maxCores":32,"maxMemory":1024,"maxDisk":2048,"maxUses":100,` +
						`"machineUniquenessStrategy":"UNIQUE_PER_ACCOUNT",` +
						`"machineMatchingStrategy":"MATCH_ALL",` +
						`"componentUniquenessStrategy":"UNIQUE_PER_LICENSE",` +
						`"componentMatchingStrategy":"MATCH_TWO",` +
						`"expirationStrategy":"REVOKE_ACCESS","expirationBasis":"FROM_FIRST_USE",` +
						`"renewalBasis":"FROM_NOW","transferStrategy":"RESET_EXPIRY",` +
						`"authenticationStrategy":"MIXED","machineLeasingStrategy":"PER_USER",` +
						`"processLeasingStrategy":"PER_USER","overageStrategy":"ALLOW_2X_OVERAGE",` +
						`"requireProductScope":true,"requirePolicyScope":true,` +
						`"requireMachineScope":true,"requireFingerprintScope":true,` +
						`"requireComponentsScope":true,"requireUserScope":true,` +
						`"requireChecksumScope":true,"requireVersionScope":true,` +
						`"requireCheckIn":true,"checkInInterval":"month","checkInIntervalCount":2,` +
						`"heartbeatDuration":120,"heartbeatCullStrategy":"KEEP_DEAD",` +
						`"heartbeatResurrectionStrategy":"ALWAYS_REVIVE","requireHeartbeat":true,` +
						`"metadata":{"x_y":1}},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pfull_id}}"}}}}}`,
					Capture: map[string]string{
						"pfull_policy": "data.id",
					},
				},
				{
					Name:   "show full policy",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pfull_policy}}",
					Auth:   admin,
				},
				{
					Name:   "policy product show",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pfull_policy}}/product",
					Auth:   admin,
				},
			},
		},
		{
			Name: "policy_index_filter",
			Steps: []conformance.Step{
				product("pfilter"),
				policy("pfilter1", "pfilter_id"),
				policy("pfilter2", "pfilter_id"),
				{
					Name:   "index filtered by product",
					Method: http.MethodGet,
					Path:   account + "/policies?product={{pfilter_id}}",
					Auth:   admin,
				},
				{
					Name:   "index filtered by unknown product",
					Method: http.MethodGet,
					Path:   account + "/policies?product=00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "index with bad filter value",
					Method: http.MethodGet,
					Path:   account + "/policies?product=notauuid",
					Auth:   admin,
				},
				{
					Name:   "index paginated",
					Method: http.MethodGet,
					Path:   account + "/policies?product={{pfilter_id}}&page[size]=1&page[number]=1",
					Auth:   admin,
				},
			},
		},
		{
			Name: "policy_pool",
			Steps: []conformance.Step{
				product("ppool"),
				pooled("ppoolpol", "ppool_id"),
				policy("ppoolplain", "ppool_id"),
				key("ppool_k1", "ppoolpol_id", "b1-pool-{{run}}-00000001-aaaaaaaa"),
				key("ppool_k2", "ppoolpol_id", "b1-pool-{{run}}-00000002-bbbbbbbb"),
				{
					Name:   "pool index",
					Method: http.MethodGet,
					Path:   account + "/policies/{{ppoolpol_id}}/pool",
					Auth:   admin,
				},
				{
					Name:   "pool key show",
					Method: http.MethodGet,
					Path:   account + "/policies/{{ppoolpol_id}}/pool/{{ppool_k1_id}}",
					Auth:   admin,
				},
				{
					Name:   "pool key show unknown",
					Method: http.MethodGet,
					Path:   account + "/policies/{{ppoolpol_id}}/pool/00000000-0000-0000-0000-000000000000",
					Auth:   admin,
				},
				{
					Name:   "pool index of unpooled policy",
					Method: http.MethodGet,
					Path:   account + "/policies/{{ppoolplain_id}}/pool",
					Auth:   admin,
				},
				{
					Name:   "pop newest",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{ppoolpol_id}}/pool",
					Auth:   admin,
				},
				{
					Name:   "pop last",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{ppoolpol_id}}/pool",
					Auth:   admin,
				},
				{
					Name:   "pop empty",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{ppoolpol_id}}/pool",
					Auth:   admin,
				},
				{
					Name:   "pop unpooled",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{ppoolplain_id}}/pool",
					Auth:   admin,
				},
			},
		},
		{
			Name: "policy_entitlements",
			Steps: []conformance.Step{
				product("pent"),
				policy("pentpol", "pent_id"),
				entitlement("pentent"),
				{
					Name:   "attach",
					Method: http.MethodPost,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{pentent_id}}"}]}`,
				},
				{
					Name:   "attach again",
					Method: http.MethodPost,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{pentent_id}}"}]}`,
				},
				{
					Name:   "attach missing",
					Method: http.MethodPost,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"00000000-0000-0000-0000-000000000000"}]}`,
				},
				{
					Name:   "attach empty array",
					Method: http.MethodPost,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[]}`,
				},
				{
					Name:   "index entitlements",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
				},
				{
					Name:   "show entitlement by id",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pentpol_id}}/entitlements/{{pentent_id}}",
					Auth:   admin,
				},
				{
					Name:   "show entitlement by code",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pentpol_id}}/entitlements/pent-{{run}}",
					Auth:   admin,
				},
				{
					Name:   "detach",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{pentent_id}}"}]}`,
				},
				{
					Name:   "detach again",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{pentpol_id}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{pentent_id}}"}]}`,
				},
			},
		},
		{
			Name: "policy_authz_product_bearer",
			Steps: []conformance.Step{
				product("pown"),
				product("pother"),
				{
					Name:   "generate product token",
					Method: http.MethodPost,
					Path:   account + "/products/{{pown_id}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token"}}`,
					Capture: map[string]string{
						"ptoken": "data.attributes.token",
					},
				},
				policy("pown_pol", "pown_id"),
				{
					Name:   "show own policy",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pown_pol_id}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "create policy for own product",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
					Body: `{"data":{"type":"policy","attributes":{"name":"pown2-{{run}}"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pown_id}}"}}}}}`,
					Capture: map[string]string{
						"pown2": "data.id",
					},
				},
				{
					Name:   "create policy for other product",
					Method: http.MethodPost,
					Path:   account + "/policies",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
					Body: `{"data":{"type":"policy","attributes":{"name":"pother-{{run}}"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{pother_id}}"}}}}}`,
				},
				{
					Name:   "update own policy",
					Method: http.MethodPatch,
					Path:   account + "/policies/{{pown2}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
					Body:   `{"data":{"type":"policy","attributes":{"name":"pown2-renamed-{{run}}"}}}`,
				},
				{
					Name:   "destroy own policy",
					Method: http.MethodDelete,
					Path:   account + "/policies/{{pown2}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
				{
					Name:   "policy product show",
					Method: http.MethodGet,
					Path:   account + "/policies/{{pown_pol_id}}/product",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "ptoken"},
				},
			},
		},
	}
}
