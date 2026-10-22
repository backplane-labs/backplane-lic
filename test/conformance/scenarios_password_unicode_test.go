package conformance_test

import (
	"net/http"
	"strings"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func unicodePasswordScenario() conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug
	password := strings.Repeat("é", 40)
	prefix := strings.Repeat("é", 36)

	return conformance.Scenario{
		Name: "password_unicode_bcrypt",
		Steps: []conformance.Step{
			{
				Name: "character-valid password longer than 72 bytes", Method: http.MethodPost,
				Path: account + "/users", Auth: admin,
				Body: `{"data":{"type":"users","attributes":{"email":"unicode-password-{{run}}@example.com",` +
					`"password":"` + password + `"}}}`,
			},
			{
				Name: "multibyte password authenticates", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{
					Kind:      conformance.AuthBasic,
					BasicUser: "unicode-password-{{run}}@example.com", BasicPass: password,
				},
				Body: `{"data":{"type":"tokens"}}`,
			},
			{
				Name: "Ruby bcrypt ignores bytes beyond its 72-byte input", Method: http.MethodPost, Path: account + "/tokens",
				Auth: conformance.Auth{
					Kind:      conformance.AuthBasic,
					BasicUser: "unicode-password-{{run}}@example.com", BasicPass: prefix + "other",
				},
				Body: `{"data":{"type":"tokens"}}`,
			},
		},
	}
}
