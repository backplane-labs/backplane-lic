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
				{
					Name:   "ban",
					Method: http.MethodPost,
					Path:   account + "/users/{{banmeId}}/actions/ban",
					Auth:   admin,
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
			Name: "users_relationship_indexes",
			Steps: []conformance.Step{
				createUser("rel"),
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
			},
		},
	}
}
