package conformance_test

import (
	"net/http"
	"strings"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// licensesScenarios covers the license resource: CRUD with key
// generation, the relationship routes, the actions (validate,
// validate-key, revoke, renew, suspend, reinstate, check-in, usage,
// check-out), license-key authentication, /me for product and license
// bearers, and the metadata limits. Bearers are admin, product and
// license only (user authentication is out of scope).
func licensesScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	licenses := account + "/licenses"
	products := account + "/products"
	policies := account + "/policies"
	users := account + "/users"
	groups := account + "/groups"
	entitlements := account + "/entitlements"

	// productSetup creates a product and captures its id.
	productSetup := []conformance.Step{
		{
			Name:   "create product",
			Method: http.MethodPost,
			Path:   products,
			Auth:   admin,
			Body:   `{"data":{"type":"product","attributes":{"name":"lic-probe-{{run}}"}}}`,
			Capture: map[string]string{
				"product": "data.id",
			},
		},
	}

	// policySetup creates a policy over the probe product; extra holds
	// additional policy attributes.
	policySetup := func(extra string) []conformance.Step {
		return append(productSetup, conformance.Step{
			Name:   "create policy",
			Method: http.MethodPost,
			Path:   policies,
			Auth:   admin,
			Body: `{"data":{"type":"policy","attributes":{"name":"lic-policy-{{run}}"` + extra +
				`},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{
				"policy": "data.id",
			},
		})
	}

	return []conformance.Scenario{
		{
			Name: "license_crud",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"name":"b3-{{run}}","metadata":{"fooBar":"baz"}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
						"key":     "data.attributes.key",
					},
				},
				{
					Name:   "show by id",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
				},
				{
					Name:   "show by key",
					Method: http.MethodGet,
					Path:   licenses + "/{{key}}",
					Auth:   admin,
				},
				{
					Name:   "unknown license",
					Method: http.MethodGet,
					Path:   licenses + "/c3a3a3a3-1111-4111-8111-111111111111",
					Auth:   admin,
				},
				{
					Name:   "update",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
					Body:   `{"data":{"type":"license","id":"{{license}}","attributes":{"name":"renamed-{{run}}","maxUses":5,"metadata":{"renamed":true}}}}`,
				},
				{
					Name:   "destroy",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
				},
				{
					Name:   "gone",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_create_validation",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "missing policy relationship",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"name":"x"}}}`,
				},
				{
					Name:   "unknown policy",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"c3a3a3a3-1111-4111-8111-111111111111"}}}}}`,
				},
				{
					Name:   "custom key too short",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"abc"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "custom key reserved",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"actions"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "unpermitted attribute",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"notAnAttribute":1},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "create for key conflict",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"dup-key-{{run}}"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "duplicate key",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"dup-key-{{run}}"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "create patch target",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "key not updatable",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"another-key-{{run}}"}}}`,
				},
			}...),
		},
		{
			Name: "license_key_schemes",
			Steps: append(productSetup, []conformance.Step{
				{
					Name:   "create ed25519 policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"ed25519-{{run}}","scheme":"ED25519_SIGN"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"edPolicy": "data.id",
					},
				},
				{
					Name:   "create rsa sign v2 policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"rsav2-{{run}}","scheme":"RSA_2048_PKCS1_SIGN_V2"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"rsaPolicy": "data.id",
					},
				},
				{
					Name:   "create rsa pss v2 policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"pssv2-{{run}}","scheme":"RSA_2048_PKCS1_PSS_SIGN_V2"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"pssPolicy": "data.id",
					},
				},
				{
					Name:   "create jwt policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"jwt-{{run}}","scheme":"RSA_2048_JWT_RS256"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"jwtPolicy": "data.id",
					},
				},
				{
					Name:   "create encrypt policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"enc-{{run}}","scheme":"RSA_2048_PKCS1_ENCRYPT"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"encPolicy": "data.id",
					},
				},
				{
					Name:   "ed25519 license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{edPolicy}}"}}}}}`,
				},
				{
					Name:   "rsa sign v2 license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{rsaPolicy}}"}}}}}`,
				},
				{
					Name:   "rsa pss v2 license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{pssPolicy}}"}}}}}`,
				},
				{
					Name:   "jwt license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{jwtPolicy}}"}}}}}`,
				},
				{
					Name:   "custom jwt claims key",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"key":"{\"not\":\"json claims\"}x"},"relationships":{"policy":{"data":{"type":"policy","id":"{{jwtPolicy}}"}}}}}`,
				},
				{
					Name:   "encrypt license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{encPolicy}}"}}}}}`,
				},
			}...),
		},
		{
			Name: "license_pooled_keys",
			Steps: append(productSetup, []conformance.Step{
				{
					Name:   "create pooled policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"pool-{{run}}","usePool":true},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"poolPolicy": "data.id",
					},
				},
				{
					Name:   "add pool key",
					Method: http.MethodPost,
					Path:   account + "/keys",
					Auth:   admin,
					Body:   `{"data":{"type":"key","attributes":{"key":"pooled-key-{{run}}"},"relationships":{"policy":{"data":{"type":"policy","id":"{{poolPolicy}}"}}}}}`,
				},
				{
					Name:   "license pops the pooled key",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{poolPolicy}}"}}}}}`,
				},
				{
					Name:   "empty pool",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{poolPolicy}}"}}}}}`,
				},
			}...),
		},
		{
			Name: "license_expiry_and_status",
			Steps: append(policySetup(`,"duration":86400`), []conformance.Step{
				{
					Name:   "create plain policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"plain-{{run}}"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"plainPolicy": "data.id",
					},
				},
				{
					Name:   "create with duration",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "create already expired",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"expiry":"2020-01-01T00:00:00Z"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"expired": "data.id",
					},
				},
				{
					Name:   "renew without expiry anchor",
					Method: http.MethodPost,
					Path:   licenses + "/{{expired}}/actions/renew",
					Auth:   admin,
				},
				{
					Name:   "renew",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/renew",
					Auth:   admin,
				},
				{
					Name:   "renew a no-duration license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{plainPolicy}}"}}}}}`,
					Capture: map[string]string{
						"plainLicense": "data.id",
					},
				},
				{
					Name:   "renew without duration",
					Method: http.MethodPost,
					Path:   licenses + "/{{plainLicense}}/actions/renew",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_validation",
			Steps: append(policySetup(`,"strict":true,"maxUses":10`), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
						"key":     "data.attributes.key",
					},
				},
				{
					Name:   "strict without machines",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   admin,
				},
				{
					Name:   "scope product mismatch",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   admin,
					Body:   `{"meta":{"nonce":42,"scope":{"product":"c3a3a3a3-1111-4111-8111-111111111111"}}}`,
				},
				{
					Name:   "scope product match",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   admin,
					Body:   `{"meta":{"nonce":7,"scope":{"product":"{{product}}"}}}`,
				},
				{
					Name:   "unpermitted scope key",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   admin,
					Body:   `{"meta":{"scope":{"environment":"production"}}}`,
				},
				{
					Name:   "validate-key anonymous",
					Method: http.MethodPost,
					Path:   licenses + "/actions/validate-key",
					Body:   `{"meta":{"key":"{{key}}","nonce":9}}`,
				},
				{
					Name:   "validate-key unknown",
					Method: http.MethodPost,
					Path:   licenses + "/actions/validate-key",
					Body:   `{"meta":{"key":"unknown-key-{{run}}"}}`,
				},
				{
					Name:   "validate-key missing key",
					Method: http.MethodPost,
					Path:   licenses + "/actions/validate-key",
					Body:   `{"meta":{"key":""}}`,
				},
				{
					Name:   "validate-key legacy form",
					Method: http.MethodPost,
					Path:   licenses + "/actions/validate-key",
					Body:   `{"meta":{"key":"{{key}}","legacyEncrypted":true}}`,
				},
			}...),
		},
		{
			Name: "license_actions",
			Steps: append(policySetup(`,"requireCheckIn":true,"checkInInterval":"day","checkInIntervalCount":1`), []conformance.Step{
				{
					Name:   "create plain policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"plain-act-{{run}}"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"plainPolicy": "data.id",
					},
				},
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "check-in",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-in",
					Auth:   admin,
				},
				{
					Name:   "create no-checkin license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{plainPolicy}}"}}}}}`,
					Capture: map[string]string{
						"plainLicense": "data.id",
					},
				},
				{
					Name:   "check-in not required",
					Method: http.MethodPost,
					Path:   licenses + "/{{plainLicense}}/actions/check-in",
					Auth:   admin,
				},
				{
					Name:   "suspend",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/suspend",
					Auth:   admin,
				},
				{
					Name:   "suspend again",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/suspend",
					Auth:   admin,
				},
				{
					Name:   "validate suspended",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   admin,
				},
				{
					Name:   "reinstate",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/reinstate",
					Auth:   admin,
				},
				{
					Name:   "reinstate again",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/reinstate",
					Auth:   admin,
				},
				{
					Name:   "increment",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/increment-usage",
					Auth:   admin,
					Body:   `{"meta":{"increment":3}}`,
				},
				{
					Name:   "decrement",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/decrement-usage",
					Auth:   admin,
				},
				{
					Name:   "reset",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/reset-usage",
					Auth:   admin,
				},
				{
					Name:   "revoke",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/actions/revoke",
					Auth:   admin,
				},
				{
					Name:   "gone",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_usage_limit",
			Steps: append(policySetup(`,"maxUses":2`), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "increment within limit",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/increment-usage",
					Auth:   admin,
					Body:   `{"meta":{"increment":2}}`,
				},
				{
					Name:   "increment beyond limit",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/increment-usage",
					Auth:   admin,
				},
				{
					Name:   "decrement below zero",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/decrement-usage",
					Auth:   admin,
					Body:   `{"meta":{"decrement":99}}`,
				},
			}...),
		},
		{
			Name: "license_relationships",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create scheme policy",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   admin,
					Body:   `{"data":{"type":"policy","attributes":{"name":"scheme-{{run}}","scheme":"ED25519_SIGN"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"schemePolicy": "data.id",
					},
				},
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   users,
					Auth:   admin,
					Body:   `{"data":{"type":"user","attributes":{"email":"lic-owner-{{run}}@example.com","firstName":"Lic","lastName":"Owner"}}}`,
					Capture: map[string]string{
						"owner": "data.id",
					},
				},
				{
					Name:   "create group",
					Method: http.MethodPost,
					Path:   groups,
					Auth:   admin,
					Body:   `{"data":{"type":"group","attributes":{"name":"lic-group-{{run}}"}}}`,
					Capture: map[string]string{
						"group": "data.id",
					},
				},
				{
					Name:   "create ownerless license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "owner null",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/owner",
					Auth:   admin,
				},
				{
					Name:   "group missing",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/group",
					Auth:   admin,
				},
				{
					Name:   "set owner",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}/owner",
					Auth:   admin,
					Body:   `{"data":{"type":"user","id":"{{owner}}"}}`,
				},
				{
					Name:   "owner show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/owner",
					Auth:   admin,
				},
				{
					Name:   "v1.5 user alias",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/user",
					Auth:   admin,
				},
				{
					Name:   "set group",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"group","id":"{{group}}"}}`,
				},
				{
					Name:   "group show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/group",
					Auth:   admin,
				},
				{
					Name:   "product show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/product",
					Auth:   admin,
				},
				{
					Name:   "policy show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/policy",
					Auth:   admin,
				},
				{
					Name:   "transfer to incompatible policy",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}/policy",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","id":"{{schemePolicy}}"}}`,
				},
				{
					Name:   "transfer to same policy",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}/policy",
					Auth:   admin,
					Body:   `{"data":{"type":"policy","id":"{{policy}}"}}`,
				},
			}...),
		},
		{
			Name: "license_users_entitlements",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create owner",
					Method: http.MethodPost,
					Path:   users,
					Auth:   admin,
					Body:   `{"data":{"type":"user","attributes":{"email":"lic-attach-owner-{{run}}@example.com"}}}`,
					Capture: map[string]string{
						"owner": "data.id",
					},
				},
				{
					Name:   "create licensee",
					Method: http.MethodPost,
					Path:   users,
					Auth:   admin,
					Body:   `{"data":{"type":"user","attributes":{"email":"lic-attach-user-{{run}}@example.com"}}}`,
					Capture: map[string]string{
						"licensee": "data.id",
					},
				},
				{
					Name:   "create entitlement",
					Method: http.MethodPost,
					Path:   entitlements,
					Auth:   admin,
					Body:   `{"data":{"type":"entitlement","attributes":{"name":"feat-{{run}}","code":"FEAT-{{run}}"}}}`,
					Capture: map[string]string{
						"entitlement": "data.id",
					},
				},
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}},"owner":{"data":{"type":"user","id":"{{owner}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "users index",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
				},
				{
					Name:   "attach licensee",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{licensee}}"}]}`,
				},
				{
					Name:   "attach owner conflict",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{owner}}"}]}`,
				},
				{
					Name:   "attach duplicate",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{licensee}}"}]}`,
				},
				{
					Name:   "detach owner forbidden",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{owner}}"}]}`,
				},
				{
					Name:   "user show by id",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/users/{{licensee}}",
					Auth:   admin,
				},
				{
					Name:   "attach entitlement",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{entitlement}}"}]}`,
				},
				{
					Name:   "entitlements index",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/entitlements",
					Auth:   admin,
				},
				{
					Name:   "entitlement show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/entitlements/{{entitlement}}",
					Auth:   admin,
				},
				{
					Name:   "detach entitlement",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{entitlement}}"}]}`,
				},
				{
					Name:   "detach unattached entitlement",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/entitlements",
					Auth:   admin,
					Body:   `{"data":[{"type":"entitlement","id":"{{entitlement}}"}]}`,
				},
				{
					Name:   "detach licensee",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{licensee}}"}]}`,
				},
				{
					Name:   "detach unattached user",
					Method: http.MethodDelete,
					Path:   licenses + "/{{license}}/users",
					Auth:   admin,
					Body:   `{"data":[{"type":"user","id":"{{licensee}}"}]}`,
				},
			}...),
		},
		{
			Name: "license_tokens",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "generate activation token",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token","attributes":{"name":"activation-{{run}}","maxActivations":3}}}`,
					Capture: map[string]string{
						"activationToken": "data.attributes.token",
						"tokenId":         "data.id",
					},
				},
				{
					Name:   "tokens index",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/tokens",
					Auth:   admin,
				},
				{
					Name:   "token show",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/tokens/{{tokenId}}",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_checkout",
			Steps: append(policySetup(`,"duration":86400`), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"metadata":{"edition":"pro"}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "checkout with includes",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"ttl":3600,"include":["product","policy"]}}`,
				},
				{
					Name:   "checkout defaults",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{}`,
				},
				{
					Name:   "checkout encrypted",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"encrypt":true,"ttl":7200}}`,
				},
				{
					Name:   "checkout signed rsa",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"algorithm":"base64+rsa-sha256","ttl":3600}}`,
				},
				{
					Name:   "checkout invalid algorithm",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"algorithm":"rot13+ed25519"}}`,
				},
				{
					Name:   "checkout ttl too small",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"ttl":60}}`,
				},
				{
					Name:   "checkout invalid include",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/check-out",
					Auth:   admin,
					Body:   `{"meta":{"include":["machines"]}}`,
				},
				{
					Name:   "checkout binary",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/actions/check-out?ttl=3600",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_index_filters",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create license a",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"name":"filter-a-{{run}}","metadata":{"tier":"gold","run":"{{run}}"}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"licenseA": "data.id",
					},
				},
				{
					Name:   "create license b",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"name":"filter-b-{{run}}","metadata":{"tier":"silver","run":"{{run}}"}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "filter by policy",
					Method: http.MethodGet,
					Path:   licenses + "?policy={{policy}}&limit=100",
					Auth:   admin,
				},
				{
					Name:   "filter by product",
					Method: http.MethodGet,
					Path:   licenses + "?product={{product}}&limit=100",
					Auth:   admin,
				},
				{
					Name:   "filter by metadata",
					Method: http.MethodGet,
					Path:   licenses + "?metadata[tier]=gold&metadata[run]={{run}}",
					Auth:   admin,
				},
				{
					Name:   "filter by unknown status",
					Method: http.MethodGet,
					Path:   licenses + "?status=NOPE",
					Auth:   admin,
				},
				{
					Name:   "offset pagination",
					Method: http.MethodGet,
					Path:   licenses + "?policy={{policy}}&page[number]=1&page[size]=1",
					Auth:   admin,
				},
				{
					Name:   "bad order",
					Method: http.MethodGet,
					Path:   licenses + "?order=sideways",
					Auth:   admin,
				},
				{
					Name:   "nested policy licenses",
					Method: http.MethodGet,
					Path:   policies + "/{{policy}}/licenses?limit=100",
					Auth:   admin,
				},
				{
					Name:   "nested policy license show",
					Method: http.MethodGet,
					Path:   policies + "/{{policy}}/licenses/{{licenseA}}",
					Auth:   admin,
				},
				{
					Name:   "nested product license miss",
					Method: http.MethodGet,
					Path:   policies + "/{{policy}}/licenses/c3a3a3a3-1111-4111-8111-111111111111",
					Auth:   admin,
				},
			}...),
		},
		{
			Name: "license_bearer_auth",
			Steps: append(policySetup(`,"authenticationStrategy":"MIXED","maxUses":5`), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
						"key":     "data.attributes.key",
					},
				},
				{
					Name:   "create other license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"other": "data.id",
					},
				},
				{
					Name:   "show self by key",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "show other 404",
					Method: http.MethodGet,
					Path:   licenses + "/{{other}}",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "me",
					Method: http.MethodGet,
					Path:   account + "/me",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "profile",
					Method: http.MethodGet,
					Path:   account + "/profile",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "validate self",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}/actions/validate",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "usage increment",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/increment-usage",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "usage decrement denied",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/decrement-usage",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "update denied",
					Method: http.MethodPatch,
					Path:   licenses + "/{{license}}",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
					Body:   `{"data":{"type":"license","attributes":{"name":"nope"}}}`,
				},
				{
					Name:   "index denied",
					Method: http.MethodGet,
					Path:   licenses,
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "suspend the license",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/suspend",
					Auth:   admin,
				},
				{
					Name:   "suspended license auth",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
				{
					Name:   "reinstate",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/actions/reinstate",
					Auth:   admin,
				},
				{
					Name:   "token-auth license allowed again",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "key"},
				},
			}...),
		},
		{
			Name: "license_bearer_token_not_allowed",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create license",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "generate activation token",
					Method: http.MethodPost,
					Path:   licenses + "/{{license}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token"}}`,
					Capture: map[string]string{
						"activationToken": "data.attributes.token",
					},
				},
				{
					Name:   "token auth not allowed by policy",
					Method: http.MethodGet,
					Path:   licenses + "/{{license}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "activationToken"},
				},
			}...),
		},
		{
			Name: "product_bearer_me",
			Steps: append(productSetup, []conformance.Step{
				{
					Name:   "generate product token",
					Method: http.MethodPost,
					Path:   products + "/{{product}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"token"}}`,
					Capture: map[string]string{
						"productToken": "data.attributes.token",
					},
				},
				{
					Name:   "me",
					Method: http.MethodGet,
					Path:   account + "/me",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
				},
				{
					Name:   "profile",
					Method: http.MethodGet,
					Path:   account + "/profile",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
				},
				{
					Name:   "create policy as product",
					Method: http.MethodPost,
					Path:   policies,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
					Body:   `{"data":{"type":"policy","attributes":{"name":"product-owned-{{run}}"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
					Capture: map[string]string{
						"ownedPolicy": "data.id",
					},
				},
				{
					Name:   "create license under the product policy",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{ownedPolicy}}"}}}}}`,
					Capture: map[string]string{
						"productLicense": "data.id",
					},
				},
				{
					Name:   "product licenses index",
					Method: http.MethodGet,
					Path:   products + "/{{product}}/licenses?limit=100",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "productToken"},
				},
			}...),
		},
		{
			Name: "license_metadata_limits",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "depth exceeded",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"metadata":{"a":{"b":{"c":{"d":1}}}}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "too many keys",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","attributes":{"metadata":{"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8,"k9":9,"k10":10,"k11":11,"k12":12,"k13":13,"k14":14,"k15":15,"k16":16,"k17":17,"k18":18,"k19":19,"k20":20,"k21":21,"k22":22,"k23":23,"k24":24,"k25":25,"k26":26,"k27":27,"k28":28,"k29":29,"k30":30,"k31":31,"k32":32,"k33":33,"k34":34,"k35":35,"k36":36,"k37":37,"k38":38,"k39":39,"k40":40,"k41":41,"k42":42,"k43":43,"k44":44,"k45":45,"k46":46,"k47":47,"k48":48,"k49":49,"k50":50,"k51":51,"k52":52,"k53":53,"k54":54,"k55":55,"k56":56,"k57":57,"k58":58,"k59":59,"k60":60,"k61":61,"k62":62,"k63":63,"k64":64,"k65":65}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
				{
					Name:   "too large",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Prepare: func(_ testing.TB, vars map[string]string) {
						vars["blob"] = strings.Repeat("x", 17000)
					},
					Body: `{"data":{"type":"license","attributes":{"metadata":{"blob":"{{blob}}"}},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
				},
			}...),
		},
		{
			Name: "license_nested_users_groups",
			Steps: append(policySetup(""), []conformance.Step{
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   users,
					Auth:   admin,
					Body:   `{"data":{"type":"user","attributes":{"email":"lic-nested-{{run}}@example.com"}}}`,
					Capture: map[string]string{
						"owner": "data.id",
					},
				},
				{
					Name:   "create license owned",
					Method: http.MethodPost,
					Path:   licenses,
					Auth:   admin,
					Body:   `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}},"owner":{"data":{"type":"user","id":"{{owner}}"}}}}}`,
					Capture: map[string]string{
						"license": "data.id",
					},
				},
				{
					Name:   "user licenses index",
					Method: http.MethodGet,
					Path:   users + "/{{owner}}/licenses?limit=100",
					Auth:   admin,
				},
				{
					Name:   "user license show",
					Method: http.MethodGet,
					Path:   users + "/{{owner}}/licenses/{{license}}",
					Auth:   admin,
				},
				{
					Name:   "user license miss",
					Method: http.MethodGet,
					Path:   users + "/{{owner}}/licenses/c3a3a3a3-1111-4111-8111-111111111111",
					Auth:   admin,
				},
				{
					Name:   "index unauthenticated",
					Method: http.MethodGet,
					Path:   licenses,
				},
			}...),
		},
	}
}
