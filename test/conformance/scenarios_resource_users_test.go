package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// resourceUserScenarios exercises plain-user ownership and role permissions.
// An admin generates the token without a password login. Member reads avoid
// account-wide pages whose contents vary with parallel scenarios.
func resourceUserScenarios() []conformance.Scenario {
	return []conformance.Scenario{resourceUserScenario()}
}

func resourceUserScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	bearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "resourceToken"}
	account := "/v1/accounts/" + conformance.AccountSlug
	create := func(name, path, body, capture string) conformance.Step {
		step := conformance.Step{Name: name, Method: http.MethodPost, Path: account + path, Auth: admin, Body: body}
		if capture != "" {
			step.Capture = map[string]string{capture: "data.id"}
		}

		return step
	}

	steps := make([]conformance.Step, 0, 64)
	steps = append(steps,
		create("create open product", "/products", `{"data":{"type":"product","attributes":{"name":"resources-{{run}}","distributionStrategy":"OPEN"}}}`, "resourceProduct"),
		create("create closed product", "/products", `{"data":{"type":"product","attributes":{"name":"closed-{{run}}","distributionStrategy":"CLOSED"}}}`, "closedProduct"),
		create("create policy", "/policies", `{"data":{"type":"policy","attributes":{"name":"resources-{{run}}","usePool":true},"relationships":{"product":{"data":{"type":"product","id":"{{resourceProduct}}"}}}}}`, "resourcePolicy"),
		create("create entitlement", "/entitlements", `{"data":{"type":"entitlement","attributes":{"name":"resources-{{run}}","code":"resources-{{run}}"}}}`, "resourceEntitlement"),
		create("create key", "/keys", `{"data":{"type":"key","attributes":{"key":"resources-{{run}}-aaaaaaaa"},"relationships":{"policy":{"data":{"type":"policy","id":"{{resourcePolicy}}"}}}}}`, "resourceKey"),
		create("create group", "/groups", `{"data":{"type":"groups","attributes":{"name":"resources-{{run}}"}}}`, "resourceGroup"),
		create("create unrelated group", "/groups", `{"data":{"type":"groups","attributes":{"name":"unrelated-{{run}}"}}}`, "unrelatedGroup"),
		create("create endpoint", "/webhook-endpoints", `{"data":{"type":"webhook-endpoints","attributes":{"url":"https://example.com/resources?run={{run}}","subscriptions":["*"]}}}`, "resourceEndpoint"),
		create("create bearer", "/users", `{"data":{"type":"users","attributes":{"email":"resources-user-{{run}}@example.com","role":"user"},"relationships":{"group":{"data":{"type":"groups","id":"{{resourceGroup}}"}}}}}`, "resourceUser"),
		conformance.Step{
			Name: "generate bearer token as admin", Method: http.MethodPost,
			Path: account + "/users/{{resourceUser}}/tokens", Auth: admin,
			Body: `{"data":{"type":"tokens"}}`, Capture: map[string]string{"resourceToken": "data.attributes.token"},
		},
	)

	reads := []struct{ name, path string }{
		{"open product", "/products/{{resourceProduct}}"},
		{"closed product", "/products/{{closedProduct}}"},
		{"policy", "/policies/{{resourcePolicy}}"},
		{"policy product", "/policies/{{resourcePolicy}}/product"},
		{"policy entitlements", "/policies/{{resourcePolicy}}/entitlements"},
		{"entitlement", "/entitlements/{{resourceEntitlement}}"},
		{"key", "/keys/{{resourceKey}}"},
		{"group", "/groups/{{resourceGroup}}"},
		{"unrelated group", "/groups/{{unrelatedGroup}}"},
		{"group owners", "/groups/{{resourceGroup}}/owners"},
		{"member group users without ownership", "/groups/{{resourceGroup}}/users"},
		{"member group licenses without ownership", "/groups/{{resourceGroup}}/licenses"},
		{"member group machines without ownership", "/groups/{{resourceGroup}}/machines"},
		{"unrelated group users", "/groups/{{unrelatedGroup}}/users"},
		{"unrelated group owners", "/groups/{{unrelatedGroup}}/owners"},
		{"endpoint", "/webhook-endpoints/{{resourceEndpoint}}"},
		{"webhook events", "/webhook-events?events[]=not.an.event"},
		{"product index", "/products"},
		{"policy index", "/policies?product={{resourceProduct}}"},
		{"entitlement index", "/entitlements"},
		{"entitlement offset index", "/entitlements?page[number]=1&page[size]=2"},
		{"entitlement cursor index", "/entitlements?page[size]=2&page[cursor]={{resourceEntitlement}}"},
		{"key index", "/keys?policy={{resourcePolicy}}"},
		{"endpoint index", "/webhook-endpoints"},
	}
	for _, read := range reads {
		steps = append(steps, conformance.Step{Name: "read " + read.name, Method: http.MethodGet, Path: account + read.path, Auth: bearer})
	}

	steps = append(steps,
		conformance.Step{
			Name: "attach bearer as unrelated group owner", Method: http.MethodPost,
			Path: account + "/groups/{{unrelatedGroup}}/owners", Auth: admin,
			Body: `{"data":[{"type":"users","id":"{{resourceUser}}"}]}`,
		},
		conformance.Step{
			Name: "read owned group without membership", Method: http.MethodGet,
			Path: account + "/groups/{{unrelatedGroup}}", Auth: bearer,
		},
		conformance.Step{
			Name: "index member and owned groups", Method: http.MethodGet,
			Path: account + "/groups?limit=100", Auth: bearer,
		},
		conformance.Step{
			Name: "count member and owned groups", Method: http.MethodGet,
			Path: account + "/groups?page[number]=1&page[size]=1", Auth: bearer,
		},
		conformance.Step{
			Name: "owner sees empty users outside owned group", Method: http.MethodGet,
			Path: account + "/groups/{{resourceGroup}}/users", Auth: bearer,
		},
		conformance.Step{
			Name: "owner reads owned group users", Method: http.MethodGet,
			Path: account + "/groups/{{unrelatedGroup}}/users", Auth: bearer,
		},
		conformance.Step{
			Name: "owner cannot detach group owners", Method: http.MethodDelete,
			Path: account + "/groups/{{unrelatedGroup}}/owners", Auth: bearer,
			Body: `{"data":[{"type":"users","id":"{{resourceUser}}"}]}`,
		},
	)
	for _, resource := range []struct{ path, kind, attributes string }{
		{"/products/{{resourceProduct}}", "product", `{"name":"changed-{{run}}"}`},
		{"/policies/{{resourcePolicy}}", "policy", `{"name":"changed-{{run}}"}`},
		{"/entitlements/{{resourceEntitlement}}", "entitlement", `{"name":"changed-{{run}}"}`},
		{"/keys/{{resourceKey}}", "key", `{"key":"changed-{{run}}-aaaaaaaa"}`},
		{"/groups/{{resourceGroup}}", "groups", `{"name":"changed-{{run}}"}`},
		{"/webhook-endpoints/{{resourceEndpoint}}", "webhook-endpoints", `{"subscriptions":["user.created"]}`},
	} {
		steps = append(steps, conformance.Step{
			Name: "update " + resource.kind, Method: http.MethodPatch, Path: account + resource.path, Auth: bearer,
			Body: `{"data":{"type":"` + resource.kind + `","attributes":` + resource.attributes + `}}`,
		})
	}

	for _, resource := range []struct{ name, path string }{
		{"open product", "/products/{{resourceProduct}}"},
		{"closed product", "/products/{{closedProduct}}"},
		{"policy", "/policies/{{resourcePolicy}}"},
		{"entitlement", "/entitlements/{{resourceEntitlement}}"},
		{"key", "/keys/{{resourceKey}}"},
		{"group", "/groups/{{resourceGroup}}"},
		{"endpoint", "/webhook-endpoints/{{resourceEndpoint}}"},
	} {
		steps = append(steps, conformance.Step{
			Name:   "user cannot delete " + resource.name,
			Method: http.MethodDelete, Path: account + resource.path, Auth: bearer,
		})
	}

	steps = append(steps,
		conformance.Step{
			Name: "user cannot show webhook event", Method: http.MethodGet,
			Path: account + "/webhook-events/00000000-0000-0000-0000-000000000001", Auth: bearer,
		},
		conformance.Step{
			Name: "user cannot delete webhook event", Method: http.MethodDelete,
			Path: account + "/webhook-events/00000000-0000-0000-0000-000000000001", Auth: bearer,
		},
		conformance.Step{
			Name: "user cannot retry webhook event", Method: http.MethodPost,
			Path: account + "/webhook-events/00000000-0000-0000-0000-000000000001/actions/retry", Auth: bearer,
		},
		// Remove the endpoint so later scenarios do not accumulate subscriptions.
		conformance.Step{
			Name: "delete endpoint", Method: http.MethodDelete,
			Path: account + "/webhook-endpoints/{{resourceEndpoint}}", Auth: admin,
		},
	)

	return conformance.Scenario{Name: "resource_user_bearer_user", Steps: steps}
}
