package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// secondFactorsScenarios covers the TOTP second factors and their
// effect on password authentication.
func secondFactorsScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	factorBase := func(user string) string {
		return account + "/users/{{" + user + "Id}}/second-factors"
	}

	return []conformance.Scenario{
		{
			Name: "second_factor_lifecycle",
			Steps: []conformance.Step{
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"sf-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{
						"sfId":    "data.id",
						"sfEmail": "data.attributes.email",
					},
				},
				{
					Name:   "create with wrong password",
					Method: http.MethodPost,
					Path:   factorBase("sf"),
					Auth:   admin,
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"wrong-{{run}}"}}`,
				},
				{
					Name:   "create with right password",
					Method: http.MethodPost,
					Path:   factorBase("sf"),
					Auth:   admin,
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
					Capture: map[string]string{
						"factorId": "data.id",
						"secret":   "data.attributes.secret",
					},
				},
				{
					Name:   "index shows the disabled factor",
					Method: http.MethodGet,
					Path:   factorBase("sf"),
					Auth:   admin,
				},
				{
					Name:   "enable without otp",
					Method: http.MethodPatch,
					Path:   factorBase("sf") + "/{{factorId}}",
					Auth:   admin,
					Body:   `{"data":{"type":"second-factors","attributes":{"enabled":true}},"meta":{"otp":"000000"}}`,
				},
				{
					Name:    "enable with otp",
					Method:  http.MethodPatch,
					Path:    factorBase("sf") + "/{{factorId}}",
					Auth:    admin,
					Prepare: conformance.OTPVar("secret", "otp"),
					Body:    `{"data":{"type":"second-factors","attributes":{"enabled":true}},"meta":{"otp":"{{otp}}"}}`,
				},
				{
					Name:   "show hides the secret",
					Method: http.MethodGet,
					Path:   factorBase("sf") + "/{{factorId}}",
					Auth:   admin,
				},
				{
					Name:   "token without otp",
					Method: http.MethodPost,
					Path:   account + "/tokens",
					Auth: conformance.Auth{
						Kind:      conformance.AuthBasic,
						BasicUser: "{{sfEmail}}",
						BasicPass: "password-{{run}}",
					},
					Body: `{"data":{"type":"tokens"}}`,
				},
				{
					Name:   "token with otp",
					Method: http.MethodPost,
					Path:   account + "/tokens",
					Auth: conformance.Auth{
						Kind:      conformance.AuthBasic,
						BasicUser: "{{sfEmail}}",
						BasicPass: "password-{{run}}",
					},
					Prepare: conformance.OTPFresh("secret", "otp"),
					Body:    `{"data":{"type":"tokens"},"meta":{"otp":"{{otp}}"}}`,
					Capture: map[string]string{"userToken": "data.attributes.token"},
				},
				{
					Name:   "token authn is unaffected",
					Method: http.MethodGet,
					Path:   account + "/profile",
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "userToken"},
				},
			},
		},
		{
			// A separate user keeps the OTP-attempt count per user below
			// the reference's 5/min throttle (a longer single sequence
			// would make the golden window-dependent).
			Name: "second_factor_delete",
			Steps: []conformance.Step{
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"sfd-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"sfdId": "data.id"},
				},
				{
					Name:   "create factor",
					Method: http.MethodPost,
					Path:   factorBase("sfd"),
					Auth:   admin,
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
					Capture: map[string]string{
						"factorId": "data.id",
						"secret":   "data.attributes.secret",
					},
				},
				{
					Name:    "enable with otp",
					Method:  http.MethodPatch,
					Path:    factorBase("sfd") + "/{{factorId}}",
					Auth:    admin,
					Prepare: conformance.OTPFresh("secret", "otp"),
					Body:    `{"data":{"type":"second-factors","attributes":{"enabled":true}},"meta":{"otp":"{{otp}}"}}`,
				},
				{
					Name:    "create a second factor conflicts",
					Method:  http.MethodPost,
					Path:    factorBase("sfd"),
					Auth:    admin,
					Prepare: conformance.OTPFresh("secret", "otp"),
					Body:    `{"data":{"type":"second-factors"},"meta":{"otp":"{{otp}}"}}`,
				},
				{
					Name:    "delete without otp",
					Method:  http.MethodDelete,
					Path:    factorBase("sfd") + "/{{factorId}}",
					Auth:    admin,
					Headers: map[string]string{},
				},
				{
					Name:    "delete with otp",
					Method:  http.MethodDelete,
					Path:    factorBase("sfd") + "/{{factorId}}",
					Auth:    admin,
					Prepare: conformance.OTPFresh("secret", "otp"),
					Headers: map[string]string{"Content-Type": "application/vnd.api+json"},
					Body:    `{"meta":{"otp":"{{otp}}"}}`,
				},
				{
					Name:   "index empty after delete",
					Method: http.MethodGet,
					Path:   factorBase("sfd"),
					Auth:   admin,
				},
				{
					Name:   "show unknown factor",
					Method: http.MethodGet,
					Path:   factorBase("sfd") + "/00000000-0000-0000-0000-000000000006",
					Auth:   admin,
				},
			},
		},
		{
			Name: "second_factor_authz",
			Steps: []conformance.Step{
				{
					Name:   "create read-only user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"ro-{{run}}@example.com","role":"read-only","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"roId": "data.id"},
				},
				{
					Name:   "create plain user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"sfz-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"sfzId": "data.id"},
				},
				{
					Name:   "mint read-only token",
					Method: http.MethodPost,
					Path:   account + "/tokens",
					Auth: conformance.Auth{
						Kind:      conformance.AuthBasic,
						BasicUser: "ro-{{run}}@example.com",
						BasicPass: "password-{{run}}",
					},
					Body:    `{"data":{"type":"tokens"}}`,
					Capture: map[string]string{"roToken": "data.attributes.token"},
				},
				{
					Name:    "mint plain token",
					Method:  http.MethodPost,
					Path:    account + "/users/{{sfzId}}/tokens",
					Auth:    admin,
					Body:    `{"data":{"type":"tokens"}}`,
					Capture: map[string]string{"sfzToken": "data.attributes.token"},
				},
				{
					Name:   "read-only cannot create a factor",
					Method: http.MethodPost,
					Path:   factorBase("ro"),
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "roToken"},
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
				},
				{
					Name:   "plain user cannot create another's factor",
					Method: http.MethodPost,
					Path:   factorBase("ro"),
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "sfzToken"},
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
				},
				{
					Name:   "own factor creation succeeds",
					Method: http.MethodPost,
					Path:   factorBase("sfz"),
					Auth:   conformance.Auth{Kind: conformance.AuthTokenVar, Var: "sfzToken"},
					Body:   `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
				},
			},
		},
	}
}

// passwordsScenarios covers POST /passwords (the reset request).
func passwordsScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	return []conformance.Scenario{
		{
			Name: "passwords_reset_request",
			Steps: []conformance.Step{
				{
					Name:   "missing email",
					Method: http.MethodPost,
					Path:   account + "/passwords",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{}}`,
				},
				{
					Name:   "unknown email is still 204",
					Method: http.MethodPost,
					Path:   account + "/passwords",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"email":"nobody-{{run}}@example.com"}}`,
				},
				{
					Name:   "unpermitted meta key",
					Method: http.MethodPost,
					Path:   account + "/passwords",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"email":"nobody-{{run}}@example.com","bogus":1}}`,
				},
				{
					Name:   "create user",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"rst-{{run}}@example.com","password":"password-{{run}}"}}}`,
					Capture: map[string]string{"rstId": "data.id"},
				},
				{
					Name:   "known email is 204",
					Method: http.MethodPost,
					Path:   account + "/passwords",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"email":"rst-{{run}}@example.com","deliver":false}}`,
				},
				{
					Name:   "reset with bogus token",
					Method: http.MethodPost,
					Path:   account + "/users/{{rstId}}/actions/reset-password",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"passwordResetToken":"bogus","newPassword":"rotated-{{run}}"}}`,
				},
				{
					Name:   "reset for unknown user",
					Method: http.MethodPost,
					Path:   account + "/users/nobody-{{run}}@example.com/actions/reset-password",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"passwordResetToken":"bogus","newPassword":"rotated-{{run}}"}}`,
				},
			},
		},
	}
}
