package conformance_test

import (
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// webhookScenarios covers the webhook endpoints and events: endpoint
// CRUD with the URL/subscription/signature rules, the bearer
// authorization (a product token scopes its endpoints to its own
// product), and the events' show/retry/destroy with the events[] filter.
//
// The index routes are compared only through deterministic windows (an
// unknown events[] filter answers the empty page; the product bearer's
// index sees only this scenario's product-scoped endpoints) — the
// account-wide pages interleave with the parallel scenarios' writes and
// cannot diff stably.
func webhookScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	endpoints := account + "/webhook-endpoints"
	events := account + "/webhook-events"

	// create endpoint answers a capture for its id.
	createEndpoint := func(name, url string, subscriptions string) conformance.Step {
		return conformance.Step{
			Name:   name,
			Method: http.MethodPost,
			Path:   endpoints,
			Auth:   admin,
			Body: `{"data":{"type":"webhook-endpoints","attributes":{"url":"` + url + `"` +
				subscriptions + `}}}`,
			Capture: map[string]string{"endpointId": "data.id"},
		}
	}

	return []conformance.Scenario{
		{
			Name: "webhook_endpoints_crud",
			Steps: []conformance.Step{
				// The duplicate subscription is deduped before save.
				createEndpoint("create with deduped subscriptions",
					"https://example.com/hook?crud={{run}}",
					`,"subscriptions":["user.created","user.created"]`),
				{
					Name:   "show",
					Method: http.MethodGet,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
				{
					Name:   "update subscriptions and algorithms",
					Method: http.MethodPatch,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"subscriptions":["*"],"signatureAlgorithm":"rsa-pss-sha256","apiVersion":"1.7"}}}`,
				},
				{
					Name:   "show after update",
					Method: http.MethodGet,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
				// The reference stores a nonexistent product id verbatim
				// (no existence validation, no FK).
				{
					Name:   "create with an unknown product",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook?dead-product={{run}}","subscriptions":["user.created"]},` +
						`"relationships":{"product":{"data":{"type":"product",` +
						`"id":"00000000-0000-0000-0000-000000000000"}}}}}`,
					Capture: map[string]string{"deadId": "data.id"},
				},
				{
					Name:   "destroy the unknown-product endpoint",
					Method: http.MethodDelete,
					Path:   endpoints + "/{{deadId}}",
					Auth:   admin,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "webhook_endpoints_validation",
			Steps: []conformance.Step{
				{
					Name:   "http is refused",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body:   `{"data":{"type":"webhook-endpoints","attributes":{"url":"http://example.com/hook"}}}`,
				},
				{
					Name:   "localhost is a bad host and address",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body:   `{"data":{"type":"webhook-endpoints","attributes":{"url":"https://localhost/hook"}}}`,
				},
				{
					Name:   "unresolvable host",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://unresolvable-{{run}}.invalid/hook"}}}`,
				},
				{
					Name:   "empty subscriptions",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook","subscriptions":[]}}}`,
				},
				{
					Name:   "unknown subscription",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook","subscriptions":["not.an-event"]}}}`,
				},
				{
					Name:   "unknown signature algorithm",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook","signatureAlgorithm":"hmac-sha256"}}}`,
				},
				{
					Name:   "unsupported api version",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook","apiVersion":"9.9"}}}`,
				},
				{
					Name:   "missing url",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body:   `{"data":{"type":"webhook-endpoints","attributes":{"subscriptions":["*"]}}}`,
				},
				{
					Name:   "wrong product relationship type",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   admin,
					Body: `{"data":{"type":"webhook-endpoints","attributes":{"url":"https://example.com/hook"},` +
						`"relationships":{"product":{"data":{"type":"entitlements",` +
						`"id":"00000000-0000-0000-0000-000000000000"}}}}}`,
				},
			},
		},
		{
			Name: "webhook_endpoints_product_bearer",
			Steps: []conformance.Step{
				{
					Name:   "create product",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body: `{"data":{"type":"product","attributes":` +
						`{"name":"Webhook Product {{run}}"}}}`,
					Capture: map[string]string{"productId": "data.id"},
				},
				{
					Name:   "create another product",
					Method: http.MethodPost,
					Path:   account + "/products",
					Auth:   admin,
					Body: `{"data":{"type":"product","attributes":` +
						`{"name":"Webhook Other {{run}}"}}}`,
					Capture: map[string]string{"otherProductId": "data.id"},
				},
				{
					Name:    "mint the product's token",
					Method:  http.MethodPost,
					Path:    account + "/products/{{productId}}/tokens",
					Auth:    admin,
					Body:    `{"data":{"type":"tokens","attributes":{"name":"webhooks"}}}`,
					Capture: map[string]string{"productToken": "data.attributes.token"},
				},
				// The Zapier workaround: without the relationship the
				// endpoint is scoped to the bearer's product.
				{
					Name:   "product token scopes its own endpoint",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
					Body: `{"data":{"type":"webhook-endpoints","attributes":` +
						`{"url":"https://example.com/hook?product={{run}}"}}}`,
					Capture: map[string]string{"endpointId": "data.id"},
				},
				{
					Name:   "product token cannot scope another product",
					Method: http.MethodPost,
					Path:   endpoints,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
					Body: `{"data":{"type":"webhook-endpoints","attributes":{"url":"https://example.com/hook?other={{run}}"},` +
						`"relationships":{"product":{"data":{"type":"product","id":"{{otherProductId}}"}}}}}`,
				},
				{
					Name:   "product token indexes its endpoints",
					Method: http.MethodGet,
					Path:   endpoints + "?limit=100",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
				},
				{
					Name:   "destroy as the product bearer",
					Method: http.MethodDelete,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
				},
			},
		},
		{
			Name: "webhook_events_lifecycle",
			Steps: []conformance.Step{
				createEndpoint("create endpoint subscribed to user.created",
					"https://example.com/hook?lifecycle={{run}}",
					`,"subscriptions":["user.created"]`),
				{
					Name:   "create the user the event describes",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"wh-lifecycle-{{run}}@example.com","password":"password-{{run}}"}}}`,
				},
				{
					Name: "wait for and show the event",
					Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
						tb.Helper()

						conformance.WaitForEvent(tb, target, vars, "eventId",
							"https://example.com/hook?lifecycle={{run}}", "user.created",
							"wh-lifecycle-{{run}}@example.com")
					},
					Method: http.MethodGet,
					Path:   events + "/{{eventId}}",
					Auth:   admin,
				},
				{
					Name:   "show the event",
					Method: http.MethodGet,
					Path:   events + "/{{eventId}}",
					Auth:   admin,
				},
				{
					Name:   "retry the event",
					Method: http.MethodPost,
					Path:   events + "/{{eventId}}/actions/retry",
					Auth:   admin,
					Body:   `{}`,
				},
				{
					Name:   "destroy the event",
					Method: http.MethodDelete,
					Path:   events + "/{{eventId}}",
					Auth:   admin,
				},
				{
					Name:   "show after destroy",
					Method: http.MethodGet,
					Path:   events + "/{{eventId}}",
					Auth:   admin,
				},
				{
					Name:   "destroy the endpoint",
					Method: http.MethodDelete,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "webhook_events_filter_and_missing_endpoint",
			Steps: []conformance.Step{
				createEndpoint("create endpoint subscribed to user.created",
					"https://example.com/hook?gone={{run}}",
					`,"subscriptions":["user.created"]`),
				{
					Name:   "index with an unknown event filter",
					Method: http.MethodGet,
					Path:   events + "?events%5B%5D=not.an-event&limit=100",
					Auth:   admin,
				},
				{
					Name:   "create the user the event describes",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"wh-gone-{{run}}@example.com","password":"password-{{run}}"}}}`,
				},
				{
					Name: "wait for and show the event",
					Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
						tb.Helper()

						conformance.WaitForEvent(tb, target, vars, "eventId",
							"https://example.com/hook?gone={{run}}", "user.created",
							"wh-gone-{{run}}@example.com")
					},
					Method: http.MethodGet,
					Path:   events + "/{{eventId}}",
					Auth:   admin,
				},
				{
					Name:   "destroy the endpoint",
					Method: http.MethodDelete,
					Path:   endpoints + "/{{endpointId}}",
					Auth:   admin,
				},
				// No endpoint row and no account endpoint with the
				// snapshot URL: the retry is the generic 422.
				{
					Name:   "retry without an endpoint",
					Method: http.MethodPost,
					Path:   events + "/{{eventId}}/actions/retry",
					Auth:   admin,
					Body:   `{}`,
				},
			},
		},
	}
}
