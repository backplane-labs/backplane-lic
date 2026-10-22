package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// tokensScenarios covers the tokens resource: index, show, regenerate
// (member and the deprecated collection form) and revoke, with the
// bearer filters and per-bearer authorization.
func tokensScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	tokens := account + "/tokens"

	return []conformance.Scenario{
		{
			Name: "tokens_lifecycle",
			Steps: []conformance.Step{
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"tok-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"userId": "data.id"},
				},
				{
					Name:   "mint token for user",
					Method: http.MethodPost,
					Path:   account + "/users/{{userId}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"tokens","attributes":{"name":"scenario"}}}`,
					Capture: map[string]string{
						"tokenId": "data.id",
						"secret":  "data.attributes.token",
					},
				},
				{
					Name:   "index filtered by bearer",
					Method: http.MethodGet,
					Path:   tokens + "?bearer[type]=user&bearer[id]={{userId}}&limit=100",
					Auth:   admin,
				},
				{
					Name:   "index filtered by role",
					Method: http.MethodGet,
					Path:   tokens + "?bearer[role][]=user&bearer[id]={{userId}}",
					Auth:   admin,
				},
				{
					Name:   "show without secret",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokenId}}",
					Auth:   admin,
				},
				{
					Name:    "regenerate member",
					Method:  http.MethodPut,
					Path:    tokens + "/{{tokenId}}",
					Auth:    admin,
					Capture: map[string]string{"secret2": "data.attributes.token"},
				},
				{
					Name:   "show after regenerate",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokenId}}",
					Auth:   admin,
				},
				{
					Name:   "revoke",
					Method: http.MethodDelete,
					Path:   tokens + "/{{tokenId}}",
					Auth:   admin,
				},
				{
					Name:   "show after revoke",
					Method: http.MethodGet,
					Path:   tokens + "/{{tokenId}}",
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
				{
					Name:   "create user one",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"one-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"oneId": "data.id"},
				},
				{
					Name:   "create user two",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"two-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"twoId": "data.id"},
				},
				{
					Name:   "mint token for one",
					Method: http.MethodPost,
					Path:   account + "/users/{{oneId}}/tokens",
					Auth:   admin,
					Body:   `{"data":{"type":"tokens"}}`,
					Capture: map[string]string{
						"oneToken":   "data.attributes.token",
						"oneTokenId": "data.id",
					},
				},
				{
					Name:    "mint token for two",
					Method:  http.MethodPost,
					Path:    account + "/users/{{twoId}}/tokens",
					Auth:    admin,
					Body:    `{"data":{"type":"tokens"}}`,
					Capture: map[string]string{"twoToken": "data.attributes.token"},
				},
				{
					Name:   "user one lists own tokens",
					Method: http.MethodGet,
					Path:   account + "/users/{{oneId}}/tokens?limit=100",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "user one lists user two tokens",
					Method: http.MethodGet,
					Path:   account + "/users/{{twoId}}/tokens",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "user one regenerates two's token",
					Method: http.MethodPut,
					Path:   tokens + "/{{oneTokenId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "twoToken"},
				},
				{
					Name:   "user two regenerates own token",
					Method: http.MethodPut,
					Path:   tokens + "/{{oneTokenId}}",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oneToken"},
				},
				{
					Name:   "user revokes own token",
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
