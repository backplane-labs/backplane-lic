package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// searchScenarios covers POST /search: the typed-parameter ladder, every
// searchable resource, AND/OR, metadata type casting, pagination and the
// search policy. Terms carry the run suffix so parallel scenarios on the
// shared account never leak into the results.
func searchScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	search := account + "/search"

	query := func(name, body string) conformance.Step {
		return conformance.Step{Name: name, Method: http.MethodPost, Path: search, Auth: admin, Body: body}
	}

	setup := []conformance.Step{
		{
			Name: "create product", Method: http.MethodPost, Path: account + "/products", Auth: admin,
			Body:    `{"data":{"type":"products","attributes":{"name":"Srch{{run}} Desktop","metadata":{"tier":"gold{{run}}"}}}}`,
			Capture: map[string]string{"product_id": "data.id"},
		},
		{
			Name: "create policy", Method: http.MethodPost, Path: account + "/policies", Auth: admin,
			Body: `{"data":{"type":"policies","attributes":{"name":"Srch{{run}} Annual"},` +
				`"relationships":{"product":{"data":{"type":"products","id":"{{product_id}}"}}}}}`,
			Capture: map[string]string{"policy_id": "data.id"},
		},
		{
			Name: "create user", Method: http.MethodPost, Path: account + "/users", Auth: admin,
			Body: `{"data":{"type":"users","attributes":{"firstName":"Srch{{run}}","lastName":"Owner",` +
				`"email":"srch{{run}}@example.test","metadata":{"customerId":"cust{{run}}"}}}}`,
			Capture: map[string]string{"user_id": "data.id"},
		},
		{
			Name: "create first license", Method: http.MethodPost, Path: account + "/licenses", Auth: admin,
			Body: `{"data":{"type":"licenses","attributes":{"name":"Srch{{run}} first","metadata":{"seats":1042,"batch":"b{{run}}"}},` +
				`"relationships":{"policy":{"data":{"type":"policies","id":"{{policy_id}}"}},` +
				`"owner":{"data":{"type":"users","id":"{{user_id}}"}}}}}`,
			Capture: map[string]string{"first_id": "data.id", "first_key": "data.attributes.key"},
		},
		{
			Name: "create second license", Method: http.MethodPost, Path: account + "/licenses", Auth: admin,
			Body: `{"data":{"type":"licenses","attributes":{"name":"Srch{{run}} second","metadata":{"seats":7007,"batch":"b{{run}}"}},` +
				`"relationships":{"policy":{"data":{"type":"policies","id":"{{policy_id}}"}}}}}`,
			Capture: map[string]string{"second_id": "data.id"},
		},
		{
			Name: "create group", Method: http.MethodPost, Path: account + "/groups", Auth: admin,
			Body: `{"data":{"type":"groups","attributes":{"name":"Srch{{run}} Team"}}}`,
		},
		{
			Name: "create entitlement", Method: http.MethodPost, Path: account + "/entitlements", Auth: admin,
			Body: `{"data":{"type":"entitlements","attributes":{"name":"Srch{{run}} Export","code":"SRCH{{run}}_EXPORT"}}}`,
		},
	}

	return []conformance.Scenario{
		{
			Name: "search_validation",
			Steps: []conformance.Step{
				query("empty body", ``),
				query("missing type", `{"meta":{}}`),
				query("missing query", `{"meta":{"type":"users"}}`),
				query("blank query", `{"meta":{"type":"users","query":{}}}`),
				query("unsupported type", `{"meta":{"type":"tokens","query":{"name":"abc"}}}`),
				query("unknown type", `{"meta":{"type":"nope","query":{"name":"abc"}}}`),
				query("unsupported attribute", `{"meta":{"type":"users","query":{"foo":"abc"}}}`),
				query("camel attribute", `{"meta":{"type":"groups","query":{"metadata":{"a":"abc"}}}}`),
				query("too small", `{"meta":{"type":"users","query":{"email":"ab"}}}`),
				query("metadata not a hash", `{"meta":{"type":"users","query":{"metadata":"abc"}}}`),
				query("metadata term too small", `{"meta":{"type":"users","query":{"metadata":{"fooBar":"ab"}}}}`),
				query("unpermitted top level", `{"meta":{"type":"users","query":{"email":"abc"}},"data":{}}`),
				query("unpermitted meta member", `{"meta":{"type":"users","query":{"email":"abc"},"foo":1}}`),
				query("type not a string", `{"meta":{"type":123,"query":{"email":"abc"}}}`),
				query("op not a string", `{"meta":{"type":"users","op":1,"query":{"email":"abc"}}}`),
				query("query not an object", `{"meta":{"type":"users","query":"x"}}`),
				query("machines answer nothing", `{"meta":{"type":"machines","query":{"name":"abc"}}}`),
			},
		},
		{
			Name: "search_matches",
			Steps: append(append([]conformance.Step{}, setup...),
				query("licenses by name", `{"meta":{"type":"licenses","query":{"name":"srch{{run}}"}}}`),
				query("licenses by key", `{"meta":{"type":"licenses","query":{"key":"{{first_key}}"}}}`),
				query("licenses by id", `{"meta":{"type":"license","query":{"id":"{{second_id}}"}}}`),
				query("licenses by product name", `{"meta":{"type":"licenses","query":{"product":"srch{{run}} desk"}}}`),
				query("licenses by policy id", `{"meta":{"type":"licenses","query":{"policy":"{{policy_id}}"}}}`),
				query("licenses by owner email", `{"meta":{"type":"licenses","query":{"owner":"srch{{run}}@example"}}}`),
				query("licenses by user id", `{"meta":{"type":"licenses","query":{"user":"{{user_id}}"}}}`),
				query("licenses by cast metadata", `{"meta":{"type":"licenses","query":{"metadata":{"seats":"1042","batch":"b{{run}}"}}}}`),
				query("licenses and", `{"meta":{"type":"licenses","query":{"name":"srch{{run}}","metadata":{"seats":"7007"}}}}`),
				query("licenses or", `{"meta":{"type":"licenses","op":"or","query":{"key":"{{first_key}}","id":"{{second_id}}"}}}`),
				conformance.Step{
					Name: "licenses paged", Method: http.MethodPost, Path: search + "?page[number]=1&page[size]=1", Auth: admin,
					Body: `{"meta":{"type":"licenses","query":{"name":"srch{{run}}"}}}`,
				},
				query("users by email", `{"meta":{"type":"users","query":{"email":"srch{{run}}@"}}}`),
				query("users by full name", `{"meta":{"type":"users","query":{"fullName":"srch{{run}} own"}}}`),
				query("users by one name word", `{"meta":{"type":"users","query":{"name":"srch{{run}}"}}}`),
				query("users by metadata", `{"meta":{"type":"users","query":{"metadata":{"customerId":"cust{{run}}"}}}}`),
				query("products by name", `{"meta":{"type":"products","query":{"name":"srch{{run}}"}}}`),
				query("products by metadata", `{"meta":{"type":"products","query":{"metadata":{"tier":"gold{{run}}"}}}}`),
				query("policies by product", `{"meta":{"type":"policies","query":{"product":"{{product_id}}"}}}`),
				query("groups by name", `{"meta":{"type":"groups","query":{"name":"srch{{run}} te"}}}`),
				query("entitlements by code", `{"meta":{"type":"entitlements","query":{"code":"srch{{run}}_exp"}}}`),
				query("no matches", `{"meta":{"type":"licenses","query":{"name":"nothing{{run}}"}}}`),
			),
		},
		{
			Name: "search_policy",
			Steps: []conformance.Step{
				setup[0],
				{
					Name: "mint product token", Method: http.MethodPost, Path: account + "/products/{{product_id}}/tokens", Auth: admin,
					Capture: map[string]string{"product_token": "data.attributes.token"},
				},
				{
					Name: "product bearer is denied", Method: http.MethodPost, Path: search,
					Auth: conformance.Auth{Kind: conformance.AuthTokenVar, Var: "product_token"},
					Body: `{"meta":{"type":"users","query":{"email":"example"}}}`,
				},
				{
					Name: "anonymous is unauthorized", Method: http.MethodPost, Path: search,
					Body: `{"meta":{"type":"users","query":{"email":"example"}}}`,
				},
			},
		},
	}
}
