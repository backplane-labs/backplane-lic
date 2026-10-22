package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// tokensScenarios covers the tokens resource: index, show, regenerate
// (member and the deprecated collection form) and revoke, with the
// bearer filters and per-bearer authorization. The tokens are product
// tokens (user-bearer tokens are retired with end-user authentication).
func tokensScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	tokens := account + "/tokens"

	createProduct := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "create " + name,
			Method: http.MethodPost,
			Path:   account + "/products",
			Auth:   admin,
			Body: `{"data":{"type":"product","attributes":` +
				`{"name":"Tokens ` + name + ` {{run}}"}}}`,
			Capture: map[string]string{name + "Id": "data.id"},
		}
	}

	mint := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "mint " + name + " token",
			Method: http.MethodPost,
			Path:   account + "/products/{{" + name + "Id}}/tokens",
			Auth:   admin,
			Body:   `{"data":{"type":"tokens","attributes":{"name":"scenario"}}}`,
			Capture: map[string]string{
				name + "Token":   "data.attributes.token",
				name + "TokenId": "data.id",
			},
		}
	}

	return []conformance.Scenario{
		{
			Name: "tokens_lifecycle",
			Steps: []conformance.Step{
				createProduct("tok"),
				mint("tok"),
				{
					Name:   "index filtered by bearer",
					Method: http.MethodGet,
					Path:   tokens + "?bearer[type]=product&bearer[id]={{tokId}}&limit=100",
					Auth:   admin,
				},
				{
					Name:   "index filtered by role",
					Method: http.MethodGet,
					Path:   tokens + "?bearer[role][]=product&bearer[id]={{tokId}}",
					Auth:   admin,
				},
				{
					Name:   "show without secret",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokTokenId}}",
					Auth:   admin,
				},
				{
					Name:    "regenerate member",
					Method:  http.MethodPut,
					Path:    tokens + "/{{tokTokenId}}",
					Auth:    admin,
					Capture: map[string]string{"tokToken2": "data.attributes.token"},
				},
				{
					Name:   "show after regenerate",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokTokenId}}",
					Auth:   admin,
				},
				{
					Name:   "revoke",
					Method: http.MethodDelete,
					Path:   tokens + "/{{tokTokenId}}",
					Auth:   admin,
				},
				{
					Name:   "show after revoke",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokTokenId}}",
					Auth:   admin,
				},
				{
					Name:   "show malformed id",
					Method: http.MethodGet,
					Path:   tokens + "/not-a-uuid",
					Auth:   admin,
				},
			},
		},
		{
			Name: "tokens_authz",
			Steps: []conformance.Step{
				createProduct("one"),
				createProduct("two"),
				mint("one"),
				mint("two"),
				{
					Name:   "product one lists its tokens",
					Method: http.MethodGet,
					Path:   account + "/products/{{oneId}}/tokens?limit=100",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "product one cannot list product two tokens",
					Method: http.MethodGet,
					Path:   account + "/products/{{twoId}}/tokens",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "product two regenerates one's token",
					Method: http.MethodPut,
					Path:   tokens + "/{{oneTokenId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "twoToken"},
				},
				{
					Name:   "product regenerates its own token",
					Method: http.MethodPut,
					Path:   tokens + "/{{oneTokenId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "product revokes its own token",
					Method: http.MethodDelete,
					Path:   tokens + "/{{oneTokenId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:    "regenerate current with token authn",
					Method:  http.MethodPut,
					Path:    tokens,
					Auth:    conformance.Auth{Kind: conformance.AuthTokenVar, Var: "twoToken"},
					Capture: map[string]string{"twoToken2": "data.attributes.token"},
				},
				{
					Name:   "regenerate current again",
					Method: http.MethodPut,
					Path:   tokens,
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "twoToken2"},
				},
				{
					Name:   "unauthenticated index",
					Method: http.MethodGet,
					Path:   tokens,
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
				},
			},
		},
	}
}
