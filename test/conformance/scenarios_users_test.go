package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// usersScenarios covers the users resource group: CRUD, actions,
// relationships, validation and authorization per bearer kind.
func usersScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	account := "/v1/accounts/" + conformance.AccountSlug

	createUser := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "create " + name,
			Method: http.MethodPost,
			Path:   account + "/users",
			Auth:   admin,
			Body: `{"data":{"type":"users","attributes":{` +
				`"firstName":"First","lastName":"User","password":"password-{{run}}",` +
				`"email":"` + name + `-{{run}}@example.com","metadata":{"tier":"gold"}}}}`,
			Capture: map[string]string{
				name + "Id":    "data.id",
				name + "Email": "data.attributes.email",
			},
		}
	}

	// mintUserToken creates a token for the user via the admin.
	mintUserToken := func(name string) conformance.Step {
		return conformance.Step{
			Name:   "mint " + name + " token",
			Method: http.MethodPost,
			Path:   account + "/users/{{" + name + "Id}}/tokens",
			Auth:   admin,
			Body:   `{"data":{"type":"tokens","attributes":{"name":"` + name + `"}}}`,
			Capture: map[string]string{
				name + "Token": "data.attributes.token",
			},
		}
	}

	asUser := func(name string) conformance.Auth {
		return conformance.Auth{Kind: conformance.AuthTokenVar, Var: name + "Token"}
	}

	return []conformance.Scenario{
		{
			Name: "users_crud",
			Steps: []conformance.Step{
				createUser("crud"),
				{
					Name:   "show by id",
					Method: http.MethodGet,
					Path:   account + "/users/{{crudId}}",
					Auth:   admin,
				},
				{
					Name:   "show by email alias",
					Method: http.MethodGet,
					Path:   account + "/users/{{crudEmail}}",
					Auth:   admin,
				},
				{
					Name:   "patch names and metadata",
					Method: http.MethodPatch,
					Path:   account + "/users/{{crudId}}",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":{` +
						`"firstName":"Renamed","metadata":{"tier":"silver","extraKey":1}}}}`,
				},
				{
					Name:   "delete",
					Method: http.MethodDelete,
					Path:   account + "/users/{{crudId}}",
					Auth:   admin,
				},
				{
					Name:   "show after delete",
					Method: http.MethodGet,
					Path:   account + "/users/{{crudId}}",
					Auth:   admin,
				},
				{
					Name:   "unknown alias",
					Method: http.MethodGet,
					Path:   account + "/users/nobody-{{run}}@example.com",
					Auth:   admin,
				},
			},
		},
		{
			Name: "users_create_validation",
			Steps: []conformance.Step{
				{
					Name:   "missing email",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"firstName":"x"}}}`,
				},
				{
					Name:   "missing data",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{}`,
				},
				{
					Name:   "bad type",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"licenses","attributes":{"email":"t-{{run}}@example.com"}}}`,
				},
				{
					Name:   "unpermitted attribute",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"email":"t2-{{run}}@example.com","bogus":1}}}`,
				},
				{
					Name:   "password too short",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"email":"t3-{{run}}@example.com","password":"abc"}}}`,
				},
				{
					Name:   "email invalid",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"email":"not-an-email"}}}`,
				},
				createUser("taken"),
				{
					Name:   "email taken",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"email":"taken-{{run}}@example.com"}}}`,
				},
				{
					Name:   "invalid role",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"email":"t4-{{run}}@example.com","role":"bogus"}}}`,
				},
				{
					Name:   "admin creates read-only",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   admin,
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"ro-{{run}}@example.com","role":"read-only","password":"password-{{run}}"}}}`,
				},
				{
					Name:   "anonymous create on protected account",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"data":{"type":"users","attributes":{"email":"anon-{{run}}@example.com"}}}`,
				},
				{
					Name:   "unauthenticated index",
					Method: http.MethodGet,
					Path:   account + "/users",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
				},
			},
		},
		{
			Name: "users_role_gating",
			Steps: []conformance.Step{
				createUser("plain"),
				mintUserToken("plain"),
				{
					Name:   "user cannot send role",
					Method: http.MethodPost,
					Path:   account + "/users",
					Auth:   asUser("plain"),
					Body: `{"data":{"type":"users","attributes":` +
						`{"email":"roleless-{{run}}@example.com","role":"admin"}}}`,
				},
				{
					Name:   "user cannot send password for self",
					Method: http.MethodPatch,
					Path:   account + "/users/{{plainId}}",
					Auth:   asUser("plain"),
					Body: `{"data":{"type":"users","attributes":` +
						`{"firstName":"SelfName","password":"newpassword-1"}}}`,
				},
				{
					Name:   "user patches own name",
					Method: http.MethodPatch,
					Path:   account + "/users/{{plainId}}",
					Auth:   asUser("plain"),
					Body:   `{"data":{"type":"users","attributes":{"firstName":"SelfName"}}}`,
				},
				{
					Name:   "admin patches user password",
					Method: http.MethodPatch,
					Path:   account + "/users/{{plainId}}",
					Auth:   admin,
					Body:   `{"data":{"type":"users","attributes":{"password":"rotated-{{run}}"}}}`,
				},
			},
		},
		{
			Name: "users_index_filters",
			Steps: []conformance.Step{
				{
					Name:    "create group",
					Method:  http.MethodPost,
					Path:    account + "/groups",
					Auth:    admin,
					Body:    `{"data":{"type":"groups","attributes":{"name":"idx-{{run}}"}}}`,
					Capture: map[string]string{"groupId": "data.id"},
				},
				createUser("idxa"),
				createUser("idxb"),
				{
					Name:   "attach a",
					Method: http.MethodPut,
					Path:   account + "/users/{{idxaId}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","id":"{{groupId}}"}}`,
				},
				{
					Name:   "attach b",
					Method: http.MethodPut,
					Path:   account + "/users/{{idxbId}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","id":"{{groupId}}"}}`,
				},
				{
					Name:   "index filtered by group",
					Method: http.MethodGet,
					Path:   account + "/users?group={{groupId}}&limit=100",
					Auth:   admin,
				},
				{
					Name:   "index filtered by metadata",
					Method: http.MethodGet,
					Path:   account + "/users?metadata[tier]=gold&group={{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "index unknown status",
					Method: http.MethodGet,
					Path:   account + "/users?status=BOGUS&group={{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "index paginated",
					Method: http.MethodGet,
					Path:   account + "/users?group={{groupId}}&page[number]=1&page[size]=1",
					Auth:   admin,
				},
				{
					Name:   "index bad page",
					Method: http.MethodGet,
					Path:   account + "/users?group={{groupId}}&page[number]=101",
					Auth:   admin,
				},
				{
					Name:   "group users index",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/users?limit=100",
					Auth:   admin,
				},
				{
					Name:   "group user show",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}/users/{{idxaId}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "users_actions",
			Steps: []conformance.Step{
				createUser("banme"),
				mintUserToken("banme"),
				{
					Name:   "ban",
					Method: http.MethodPost,
					Path:   account + "/users/{{banmeId}}/actions/ban",
					Auth:   admin,
				},
				{
					Name:    "banned user token is rejected",
					Method:  http.MethodGet,
					Path:    account + "/users/{{banmeId}}",
					Auth:    asUser("banme"),
					Headers: map[string]string{},
				},
				{
					Name:   "unban",
					Method: http.MethodPost,
					Path:   account + "/users/{{banmeId}}/actions/unban",
					Auth:   admin,
				},
				{
					Name:   "ban by email alias",
					Method: http.MethodPost,
					Path:   account + "/users/{{banmeEmail}}/actions/ban",
					Auth:   admin,
				},
				{
					Name:   "ban an admin is denied",
					Method: http.MethodPost,
					Path:   account + "/users/admin@example.com/actions/ban",
					Auth:   admin,
				},
				{
					Name:   "unban again",
					Method: http.MethodPost,
					Path:   account + "/users/{{banmeId}}/actions/unban",
					Auth:   admin,
				},
			},
		},
		{
			Name: "users_passwords",
			Steps: []conformance.Step{
				createUser("pw"),
				mintUserToken("pw"),
				{
					Name:   "update-password wrong old",
					Method: http.MethodPost,
					Path:   account + "/users/{{pwId}}/actions/update-password",
					Auth:   asUser("pw"),
					Body:   `{"meta":{"oldPassword":"wrong-{{run}}","newPassword":"rotated-{{run}}"}}`,
				},
				{
					Name:   "update-password missing meta",
					Method: http.MethodPost,
					Path:   account + "/users/{{pwId}}/actions/update-password",
					Auth:   asUser("pw"),
					Body:   `{}`,
				},
				{
					Name:   "update-password right old",
					Method: http.MethodPost,
					Path:   account + "/users/{{pwId}}/actions/update-password",
					Auth:   asUser("pw"),
					Body:   `{"meta":{"oldPassword":"password-{{run}}","newPassword":"rotated-{{run}}"}}`,
				},
				{
					Name:   "old token revoked",
					Method: http.MethodGet,
					Path:   account + "/users/{{pwId}}",
					Auth:   asUser("pw"),
				},
				{
					Name:   "reset-password with bogus token",
					Method: http.MethodPost,
					Path:   account + "/users/{{pwId}}/actions/reset-password",
					Auth:   conformance.Auth{Kind: conformance.AuthNone},
					Body:   `{"meta":{"passwordResetToken":"bogus","newPassword":"newerpass-1"}}`,
				},
				{
					Name:   "update-password as admin for other user",
					Method: http.MethodPost,
					Path:   account + "/users/{{pwId}}/actions/update-password",
					Auth:   admin,
					Body:   `{"meta":{"oldPassword":"x","newPassword":"newerpass-1"}}`,
				},
			},
		},
		{
			Name: "users_group_relationship",
			Steps: []conformance.Step{
				{
					Name:   "group show without group",
					Method: http.MethodGet,
					Path:   account + "/users/admin@example.com/group",
					Auth:   admin,
				},
				{
					Name:    "create group max one user",
					Method:  http.MethodPost,
					Path:    account + "/groups",
					Auth:    admin,
					Body:    `{"data":{"type":"groups","attributes":{"name":"gr-{{run}}","maxUsers":1}}}`,
					Capture: map[string]string{"groupId": "data.id"},
				},
				createUser("grone"),
				createUser("grtwo"),
				{
					Name:   "attach first",
					Method: http.MethodPut,
					Path:   account + "/users/{{groneId}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","id":"{{groupId}}"}}`,
				},
				{
					Name:   "attach second exceeds limit",
					Method: http.MethodPut,
					Path:   account + "/users/{{grtwoId}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","id":"{{groupId}}"}}`,
				},
				{
					Name:   "group show",
					Method: http.MethodGet,
					Path:   account + "/users/{{groneId}}/group",
					Auth:   admin,
				},
				{
					Name:   "attach unknown group",
					Method: http.MethodPut,
					Path:   account + "/users/{{groneId}}/group",
					Auth:   admin,
					Body:   `{"data":{"type":"groups","id":"00000000-0000-0000-0000-000000000001"}}`,
				},
				{
					Name:   "remove group",
					Method: http.MethodPut,
					Path:   account + "/users/{{groneId}}/group",
					Auth:   admin,
					Body:   `{"data":null}`,
				},
				{
					Name:   "delete group",
					Method: http.MethodDelete,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
				},
				{
					Name:   "group show after delete",
					Method: http.MethodGet,
					Path:   account + "/groups/{{groupId}}",
					Auth:   admin,
				},
			},
		},
		{
			Name: "users_relationship_indexes",
			Steps: []conformance.Step{
				createUser("rel"),
				mintUserToken("rel"),
				{
					Name:   "products empty",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/products",
					Auth:   admin,
				},
				{
					Name:   "licenses empty",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/licenses",
					Auth:   admin,
				},
				{
					Name:   "machines empty",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/machines",
					Auth:   admin,
				},
				{
					Name:   "license show unknown",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/licenses/not-a-license",
					Auth:   admin,
				},
				{
					Name:   "tokens empty",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/tokens",
					Auth:   admin,
				},
				mintUserToken("rel"),
				{
					Name:    "tokens index has the minted token",
					Method:  http.MethodGet,
					Path:    account + "/users/{{relId}}/tokens?limit=100",
					Auth:    admin,
					Capture: map[string]string{"relTokenId": "data.0.id"},
				},
				{
					Name:   "token show without secret",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/tokens/{{relTokenId}}",
					Auth:   admin,
				},
				{
					Name:   "user token create by user is denied",
					Method: http.MethodPost,
					Path:   account + "/users/{{relId}}/tokens",
					Auth:   asUser("rel"),
					Body:   `{"data":{"type":"tokens"}}`,
				},
				{
					Name:   "tokens index of another user is denied",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}/tokens",
					Auth:   asUser("rel"),
				},
				{
					Name:   "user sees self",
					Method: http.MethodGet,
					Path:   account + "/users/{{relId}}",
					Auth:   asUser("rel"),
				},
				{
					Name:   "me",
					Method: http.MethodGet,
					Path:   account + "/me",
					Auth:   asUser("rel"),
				},
				{
					Name:   "profile",
					Method: http.MethodGet,
					Path:   account + "/profile",
					Auth:   asUser("rel"),
				},
			},
		},
	}
}
