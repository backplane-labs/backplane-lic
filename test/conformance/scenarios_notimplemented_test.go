package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// notImplementedScenarios covers the retired end-user authentication
// routes (email:password login, second factors, password change/reset
// and user-bearer token minting): every one answers the 501. This is a
// deliberate deviation from the reference (which implements them), so
// the golden is captured from this server, not recorded.
func notImplementedScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	users := account + "/users"

	basic := conformance.Auth{
		Kind:      conformance.AuthBasic,
		BasicUser: conformance.AdminEmail,
		BasicPass: conformance.AdminPass,
	}

	body := `{"data":{}}`

	return []conformance.Scenario{
		{
			Name: "end_user_auth_not_implemented",
			Steps: []conformance.Step{
				{
					Name:   "create the target user",
					Method: http.MethodPost,
					Path:   users,
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"ni-{{run}}@example.com"}}}`,
					Capture: map[string]string{"userId": "data.id"},
				},
				{
					Name:   "basic password login on tokens",
					Method: http.MethodPost,
					Path:   account + "/tokens",
					Auth:   basic,
					Body:   body,
				},
				{
					Name:   "basic password login on the account",
					Method: http.MethodGet,
					Path:   account,
					Auth:   basic,
				},
				{
					Name:   "user token minting",
					Method: http.MethodPost,
					Path:   users + "/{{userId}}/tokens",
					Auth:   admin,
					Body:   body,
				},
				{
					Name:   "token generation for a user bearer",
					Method: http.MethodPost,
					Path:   account + "/tokens",
					Auth:   admin,
					Body: `{"data":{"type":"tokens","relationships":{"bearer":` +
						`{"data":{"type":"user","id":"{{userId}}"}}}}}`,
				},
				{
					Name:   "second factors index",
					Method: http.MethodGet,
					Path:   users + "/{{userId}}/second-factors",
					Auth:   admin,
				},
				{
					Name:   "second factor create",
					Method: http.MethodPost,
					Path:   users + "/{{userId}}/second-factors",
					Auth:   admin,
					Body:   body,
				},
				{
					Name:   "password update",
					Method: http.MethodPost,
					Path:   users + "/{{userId}}/actions/update-password",
					Auth:   admin,
					Body:   `{"meta":{"newPassword":"new-password-1"}}`,
				},
				{
					Name:   "password reset",
					Method: http.MethodPost,
					Path:   users + "/{{userId}}/actions/reset-password",
					Auth:   admin,
					Body:   `{"meta":{"passwordResetToken":"bogus","newPassword":"new-password-1"}}`,
				},
				{
					Name:   "password reset request",
					Method: http.MethodPost,
					Path:   account + "/passwords",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"email":"ni-{{run}}@example.com"}}`,
				},
			},
		},
	}
}
