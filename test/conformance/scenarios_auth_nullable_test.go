package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func nullableAuthScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	basic := conformance.Auth{
		Kind:      conformance.AuthBasic,
		BasicUser: "nullable-auth-{{run}}@example.com", BasicPass: "password-{{run}}",
	}

	return conformance.Scenario{
		Name: "auth_nullable_attributes",
		Steps: []conformance.Step{
			{
				Name: "signup password is permitted before protected-account policy", Method: http.MethodPost,
				Path: account + "/users",
				Body: `{"data":{"type":"users","attributes":{"email":"signup-auth-{{run}}@example.com",` +
					`"password":"password-{{run}}"}}}`,
			},
			{
				Name: "create password user", Method: http.MethodPost, Path: account + "/users", Auth: admin,
				Body: `{"data":{"type":"users","attributes":{"email":"nullable-auth-{{run}}@example.com",` +
					`"password":"password-{{run}}"}}}`,
				Capture: map[string]string{"nullableUser": "data.id"},
			},
			{
				Name: "Basic explicit null expiry remains non-expiring", Method: http.MethodPost,
				Path: account + "/tokens", Auth: basic,
				Body:    `{"data":{"type":"tokens","attributes":{"expiry":null}}}`,
				Capture: map[string]string{"nullableBearer": "data.attributes.token"},
			},
			{
				Name: "subroute explicit null expiry remains non-expiring", Method: http.MethodPost,
				Path: account + "/users/{{nullableUser}}/tokens", Auth: admin,
				Body: `{"data":{"type":"tokens","attributes":{"expiry":null}}}`,
			},
			{
				Name: "admin clears password with null", Method: http.MethodPatch,
				Path: account + "/users/{{nullableUser}}", Auth: admin,
				Body: `{"data":{"type":"users","attributes":{"password":null}}}`,
			},
			{
				Name: "password is now unsupported", Method: http.MethodPost, Path: account + "/tokens", Auth: basic,
				Body: `{"data":{"type":"tokens"}}`,
			},
			{
				Name: "existing bearer still mints a non-expiring token", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{Kind: conformance.AuthTokenVar, Var: "nullableBearer"},
				Body: `{"data":{"type":"tokens","attributes":{"expiry":null}}}`,
			},
		},
	}
}
