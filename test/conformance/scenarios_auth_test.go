package conformance_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func authScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	basic := func(email, password string) conformance.Auth {
		return conformance.Auth{Kind: conformance.AuthBasic, BasicUser: email, BasicPass: password}
	}
	login := func(name, email, password string) conformance.Step {
		return conformance.Step{
			Name: name, Method: http.MethodPost, Path: account + "/tokens", Auth: basic(email, password),
			Body: `{"data":{"type":"tokens"}}`,
		}
	}
	createUser := func(name, email, attributes string) conformance.Step {
		return conformance.Step{
			Name: name, Method: http.MethodPost, Path: account + "/users", Auth: admin,
			Body:    `{"data":{"type":"users","attributes":{"email":"` + email + `"` + attributes + `}}}`,
			Capture: map[string]string{"userId": "data.id"},
		}
	}

	throttle := make([]conformance.Step, 0, 8)

	throttle = append(throttle, []conformance.Step{
		createUser("create factor owner", "mfa-limit-{{run}}@example.com", `,"password":"password-{{run}}"`),
		{
			Name: "create factor spends first attempt", Method: http.MethodPost,
			Path: account + "/users/{{userId}}/second-factors", Auth: admin,
			Body:    `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
			Capture: map[string]string{"factorId": "data.id"},
		},
	}...)
	for _, name := range []string{"wrong OTP one", "wrong OTP two", "wrong OTP three", "wrong OTP four", "throttled", "still throttled"} {
		throttle = append(throttle, conformance.Step{
			Name: name, Method: http.MethodPatch,
			Path: account + "/users/{{userId}}/second-factors/{{factorId}}", Auth: admin,
			Body: `{"data":{"type":"second-factors","attributes":{"enabled":true}},"meta":{"otp":"000000"}}`,
		})
	}

	return []conformance.Scenario{
		{
			Name: "auth_basic_error_order",
			Steps: []conformance.Step{
				login("malformed email without at sign uses legacy token auth", "not-an-email", "wrong"),
				login("malformed password-flow email", "broken@", "wrong"),
				login("unknown email", "unknown-auth-{{run}}@example.com", "wrong"),
				createUser("create passwordless user", "no-password-{{run}}@example.com", ""),
				login("unsupported beats required", "no-password-{{run}}@example.com", ""),
				createUser("create password user", "auth-password-{{run}}@example.com", `,"password":"password-{{run}}"`),
				login("password required", "auth-password-{{run}}@example.com", ""),
				login("whitespace password required", "auth-password-{{run}}@example.com", "   "),
				login("password invalid", "auth-password-{{run}}@example.com", "wrong"),
				{
					Name: "ban user", Method: http.MethodPost,
					Path: account + "/users/{{userId}}/actions/ban", Auth: admin,
				},
				login("invalid password beats banned", "auth-password-{{run}}@example.com", "wrong"),
				login("banned after valid password", "auth-password-{{run}}@example.com", "password-{{run}}"),
			},
		},
		{Name: "second_factor_throttle", Steps: throttle},
		passwordResetWebhookScenario(),
		secondFactorWebhookScenario(),
		tokenHierarchyScenario(),
		unicodePasswordScenario(),
		nullableAuthScenario(),
	}
}

func passwordResetWebhookScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	user := account + "/users/{{resetUser}}"
	endpoint := "https://example.com/hook?auth-reset={{run}}"

	return conformance.Scenario{
		Name: "password_reset_webhook",
		Steps: []conformance.Step{
			{
				Name: "create reset subscriber", Method: http.MethodPost, Path: account + "/webhook-endpoints", Auth: admin,
				Body: `{"data":{"type":"webhook-endpoints","attributes":{"url":"` + endpoint +
					`","subscriptions":["user.password-reset"]}}}`,
				Capture: map[string]string{"resetEndpoint": "data.id"},
			},
			{
				Name: "create user", Method: http.MethodPost, Path: account + "/users", Auth: admin,
				Body: `{"data":{"type":"users","attributes":{"email":"reset-event-{{run}}@example.com",` +
					`"password":"password-{{run}}"}}}`,
				Capture: map[string]string{"resetUser": "data.id"},
			},
			{
				Name: "mint old bearer", Method: http.MethodPost, Path: user + "/tokens", Auth: admin,
				Body: `{"data":{"type":"tokens"}}`, Capture: map[string]string{"oldResetBearer": "data.attributes.token"},
			},
			{
				Name: "request reset without email delivery", Method: http.MethodPost, Path: account + "/passwords",
				Body: `{"meta":{"email":"reset-event-{{run}}@example.com","deliver":false}}`,
			},
			{
				Name: "reset event carries plaintext claim", Method: http.MethodGet,
				Path: account + "/webhook-events/{{resetEvent}}", Auth: admin,
				Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
					tb.Helper()
					conformance.WaitForEvent(tb, target, vars, "resetEvent", endpoint, "user.password-reset", "reset-event-{{run}}")
					captureResetClaim(tb, target, vars)
				},
			},
			{
				Name: "invalid replacement does not consume claim", Method: http.MethodPost, Path: user + "/actions/reset-password",
				Body: `{"meta":{"passwordResetToken":"{{resetClaim}}","newPassword":"short"}}`,
			},
			{
				Name: "anonymous reset succeeds", Method: http.MethodPost, Path: user + "/actions/reset-password",
				Body: `{"meta":{"passwordResetToken":"{{resetClaim}}","newPassword":"rotated-{{run}}"}}`,
			},
			{
				Name: "old bearer revoked", Method: http.MethodGet, Path: account + "/profile",
				Auth: conformance.Auth{Kind: conformance.AuthTokenVar, Var: "oldResetBearer"},
			},
			{
				Name: "old password rejected", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{Kind: conformance.AuthBasic, BasicUser: "reset-event-{{run}}@example.com", BasicPass: "password-{{run}}"},
				Body: `{"data":{"type":"tokens"}}`,
			},
			{
				Name: "new password accepted", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{Kind: conformance.AuthBasic, BasicUser: "reset-event-{{run}}@example.com", BasicPass: "rotated-{{run}}"},
				Body: `{"data":{"type":"tokens"}}`,
			},
			{
				Name: "reset claim cannot replay", Method: http.MethodPost, Path: user + "/actions/reset-password",
				Body: `{"meta":{"passwordResetToken":"{{resetClaim}}","newPassword":"again-{{run}}"}}`,
			},
			{
				Name: "delete reset subscriber", Method: http.MethodDelete,
				Path: account + "/webhook-endpoints/{{resetEndpoint}}", Auth: admin,
			},
		},
	}
}

func captureResetClaim(tb testing.TB, target conformance.Target, vars map[string]string) {
	tb.Helper()

	req, err := target.NewRequest(tb.Context(), http.MethodGet,
		"/v1/accounts/"+conformance.AccountSlug+"/webhook-events/"+vars["resetEvent"], http.NoBody)
	if err != nil {
		tb.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+conformance.AdminToken(tb, target))

	resp, err := target.Do(req)
	if err != nil {
		tb.Fatal(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		tb.Fatal(err)
	}

	var event struct {
		Data struct {
			Attributes struct {
				Payload string `json:"payload"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		tb.Fatal(err)
	}

	var payload struct {
		Meta struct {
			Token string `json:"passwordResetToken"` //nolint:tagliatelle // Keygen wire spelling
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(event.Data.Attributes.Payload), &payload); err != nil {
		tb.Fatal(err)
	}

	if payload.Meta.Token == "" {
		tb.Fatal("password reset event omitted plaintext claim")
	}

	vars["resetClaim"] = payload.Meta.Token
}
