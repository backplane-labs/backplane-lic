package conformance_test

import (
	"net/http"
	"testing"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func secondFactorWebhookScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	factors := account + "/users/{{eventUser}}/second-factors"
	factor := factors + "/{{eventFactor}}"
	endpoint := "https://example.com/hook?factor-events={{run}}"
	eventStep := func(event string) conformance.Step {
		return conformance.Step{
			Name: event + " payload omits secrets", Method: http.MethodGet,
			Path: account + "/webhook-events/{{factorEvent}}", Auth: admin,
			Poll: func(tb testing.TB, target conformance.Target, vars map[string]string) {
				tb.Helper()
				conformance.WaitForEvent(tb, target, vars, "factorEvent", endpoint, event, "{{eventFactor}}")
			},
		}
	}

	return conformance.Scenario{
		Name: "second_factor_webhooks",
		Steps: []conformance.Step{
			{
				Name: "subscribe factor events", Method: http.MethodPost, Path: account + "/webhook-endpoints", Auth: admin,
				Body: `{"data":{"type":"webhook-endpoints","attributes":{"url":"` + endpoint +
					`","subscriptions":["second-factor.created","second-factor.enabled","second-factor.disabled","second-factor.deleted"]}}}`,
				Capture: map[string]string{"factorEndpoint": "data.id"},
			},
			{
				Name: "create factor owner", Method: http.MethodPost, Path: account + "/users", Auth: admin,
				Body: `{"data":{"type":"users","attributes":{"email":"factor-event-{{run}}@example.com",` +
					`"password":"password-{{run}}"}}}`,
				Capture: map[string]string{"eventUser": "data.id"},
			},
			{
				Name: "create factor", Method: http.MethodPost, Path: factors, Auth: admin,
				Body:    `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
				Capture: map[string]string{"eventFactor": "data.id", "eventSecret": "data.attributes.secret"},
			},
			eventStep("second-factor.created"),
			{
				Name: "duplicate disabled factor conflicts", Method: http.MethodPost, Path: factors, Auth: admin,
				Body: `{"data":{"type":"second-factors"},"meta":{"password":"password-{{run}}"}}`,
			},
			{
				Name: "enable factor via PUT alias", Method: http.MethodPut, Path: factor, Auth: admin,
				Prepare: conformance.OTPVar("eventSecret", "eventOTP"),
				Body:    `{"data":{"type":"second-factors","attributes":{"enabled":true}},"meta":{"otp":"{{eventOTP}}"}}`,
			},
			eventStep("second-factor.enabled"),
			{
				Name: "disable factor", Method: http.MethodPatch, Path: factor, Auth: admin,
				Prepare: conformance.OTPFresh("eventSecret", "eventOTP"),
				Body:    `{"data":{"type":"second-factors","attributes":{"enabled":false}},"meta":{"otp":"{{eventOTP}}"}}`,
			},
			eventStep("second-factor.disabled"),
			{Name: "delete disabled factor", Method: http.MethodDelete, Path: factor, Auth: admin},
			eventStep("second-factor.deleted"),
			{
				Name: "delete factor subscriber", Method: http.MethodDelete,
				Path: account + "/webhook-endpoints/{{factorEndpoint}}", Auth: admin,
			},
		},
	}
}

func tokenHierarchyScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	create := func(role, variable string) conformance.Step {
		return conformance.Step{
			Name: "create " + role, Method: http.MethodPost, Path: account + "/users", Auth: admin,
			Body: `{"data":{"type":"users","attributes":{"email":"hierarchy-` + role +
				`-{{run}}@example.com","role":"` + role + `","password":"password-{{run}}"}}}`,
			Capture: map[string]string{variable: "data.id"},
		}
	}
	mint := func(name, variable string, auth conformance.Auth) conformance.Step {
		return conformance.Step{
			Name: name, Method: http.MethodPost, Path: account + "/users/{{" + variable + "}}/tokens", Auth: auth,
			Body: `{"data":{"type":"tokens"}}`,
		}
	}
	developer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "developerBearer"}

	return conformance.Scenario{
		Name: "tokens_role_hierarchy",
		Steps: []conformance.Step{
			create("admin", "hierarchyAdmin"),
			create("developer", "hierarchyDeveloper"),
			create("user", "hierarchyUser"),
			{
				Name: "authenticate developer", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{
					Kind:      conformance.AuthBasic,
					BasicUser: "hierarchy-developer-{{run}}@example.com", BasicPass: "password-{{run}}",
				},
				Body: `{"data":{"type":"tokens"}}`, Capture: map[string]string{"developerBearer": "data.attributes.token"},
			},
			mint("admin cannot mint for another admin", "hierarchyAdmin", admin),
			mint("admin cannot mint for developer", "hierarchyDeveloper", admin),
			mint("developer cannot mint for admin", "hierarchyAdmin", developer),
			mint("developer can mint for self", "hierarchyDeveloper", developer),
			mint("developer can mint for plain user", "hierarchyUser", developer),
			{
				Name: "developer cannot override bearer to admin", Method: http.MethodPost, Path: account + "/tokens", Auth: developer,
				Body: `{"data":{"type":"tokens","relationships":{"bearer":{"data":{"type":"users","id":"{{hierarchyAdmin}}"}}}}}`,
			},
		},
	}
}
