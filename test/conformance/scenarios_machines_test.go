package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// machineKit builds the steps of the machine scenarios: every scenario
// creates its own product, policies, licenses and users.
type machineKit struct {
	admin conformance.Auth
	base  string
}

func newMachineKit() machineKit {
	return machineKit{admin: conformance.Auth{Kind: conformance.AuthAdmin}, base: "/v1/accounts/" + conformance.AccountSlug}
}

func (k machineKit) step(name, method, path, body string, auth conformance.Auth) conformance.Step {
	return conformance.Step{Name: name, Method: method, Path: k.base + path, Auth: auth, Body: body}
}

func (k machineKit) create(name, path, body, capture string) conformance.Step {
	return conformance.Step{
		Name: name, Method: http.MethodPost, Path: k.base + path, Auth: k.admin, Body: body,
		Capture: map[string]string{capture: "data.id"},
	}
}

func (k machineKit) product(capture string) conformance.Step {
	return k.create("product "+capture, "/products",
		`{"data":{"type":"product","attributes":{"name":"machines-`+capture+`-{{run}}"}}}`, capture)
}

// policy creates a policy of product with the given extra attributes
// (a JSON object body without braces, possibly empty).
func (k machineKit) policy(capture, product, attrs string) conformance.Step {
	if attrs != "" {
		attrs = "," + attrs
	}

	return k.create("policy "+capture, "/policies",
		`{"data":{"type":"policy","attributes":{"name":"machines-`+capture+`-{{run}}"`+attrs+
			`},"relationships":{"product":{"data":{"type":"product","id":"{{`+product+`}}"}}}}}`, capture)
}

// license creates a license of policy; owner may be empty.
func (k machineKit) license(capture, policy, owner string) conformance.Step {
	rels := `"policy":{"data":{"type":"policy","id":"{{` + policy + `}}"}}`
	if owner != "" {
		rels += `,"owner":{"data":{"type":"user","id":"{{` + owner + `}}"}}`
	}

	step := k.create("license "+capture, "/licenses", `{"data":{"type":"license","relationships":{`+rels+`}}}`, capture)
	step.Capture[capture+"Key"] = "data.attributes.key"

	return step
}

func (k machineKit) user(capture string) conformance.Step {
	return k.create("user "+capture, "/users",
		`{"data":{"type":"users","attributes":{"email":"machines-`+capture+`-{{run}}@example.com"}}}`, capture)
}

func (k machineKit) userToken(capture, user string) conformance.Step {
	return conformance.Step{
		Name: "token " + capture, Method: http.MethodPost, Path: k.base + "/users/{{" + user + "}}/tokens",
		Auth: k.admin, Body: `{"data":{"type":"tokens"}}`,
		Capture: map[string]string{capture: "data.attributes.token"},
	}
}

func (k machineKit) productToken(capture, product string) conformance.Step {
	return conformance.Step{
		Name: "token " + capture, Method: http.MethodPost, Path: k.base + "/products/{{" + product + "}}/tokens",
		Auth: k.admin, Body: `{"data":{"type":"tokens"}}`,
		Capture: map[string]string{capture: "data.attributes.token"},
	}
}

// machine activates fingerprint on license as auth; extra is appended to
// the attributes, rels to the relationships.
func (k machineKit) machine(name, capture, license, fingerprint, extra, rels string, auth conformance.Auth) conformance.Step {
	if extra != "" {
		extra = "," + extra
	}

	if rels != "" {
		rels = "," + rels
	}

	step := k.step(name, http.MethodPost, "/machines",
		`{"data":{"type":"machine","attributes":{"fingerprint":"`+fingerprint+`"`+extra+
			`},"relationships":{"license":{"data":{"type":"license","id":"{{`+license+`}}"}}`+rels+`}}}`, auth)
	if capture != "" {
		step.Capture = map[string]string{capture: "data.id"}
	}

	return step
}

func machineScenarios() []conformance.Scenario {
	k := newMachineKit()

	return []conformance.Scenario{
		machineCRUDScenario(k),
		machineLimitsScenario(k),
		machineLeasingScenario(k),
		machineUniquenessScenario(k),
		machineRolesScenario(k),
		machineHeartbeatScenario(k),
		machineCheckoutScenario(k),
		machineRelationshipsScenario(k),
		machineActivationTokenScenario(k),
		machineValidationScenario(k),
	}
}

// machineValidationScenario validates licenses against fresh machines:
// a machine that never pinged is in the alive scope.
func machineValidationScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	validate := func(name, license, scope string) conformance.Step {
		return k.step(name, http.MethodPost, "/licenses/{{"+license+"}}/actions/validate",
			`{"meta":{"scope":`+scope+`}}`, admin)
	}

	return conformance.Scenario{Name: "machines_validation", Steps: []conformance.Step{
		k.product("valProduct"),
		k.policy("valPolicy", "valProduct", `"floating":true,"maxMachines":5`),
		k.policy("valBeatPolicy", "valProduct", `"floating":true,"maxMachines":5,"requireHeartbeat":true,"heartbeatDuration":3600`),
		k.license("valLicense", "valPolicy", ""),
		k.license("valBeatLicense", "valBeatPolicy", ""),
		k.machine("activate", "valMachine", "valLicense", "val-1-{{run}}", "", "", admin),
		k.machine("activate second", "", "valLicense", "val-2-{{run}}", "", "", admin),
		k.machine("activate with heartbeat", "valBeatMachine", "valBeatLicense", "val-3-{{run}}", "", "", admin),
		validate("fresh fingerprint", "valLicense", `{"fingerprint":"val-1-{{run}}"}`),
		validate("fresh fingerprints", "valLicense", `{"fingerprints":["val-1-{{run}}","val-2-{{run}}"]}`),
		validate("unknown fingerprint", "valLicense", `{"fingerprint":"nope-{{run}}"}`),
		validate("fresh machine", "valLicense", `{"machine":"{{valMachine}}"}`),
		validate("heartbeat not started fingerprint", "valBeatLicense", `{"fingerprint":"val-3-{{run}}"}`),
		validate("heartbeat not started machine", "valBeatLicense", `{"machine":"{{valBeatMachine}}"}`),
		k.step("ping", http.MethodPost, "/machines/{{valBeatMachine}}/actions/ping", "", admin),
		validate("heartbeat alive fingerprint", "valBeatLicense", `{"fingerprint":"val-3-{{run}}"}`),
	}}
}

func machineCRUDScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_crud", Steps: []conformance.Step{
		k.product("crudProduct"),
		k.policy("crudPolicy", "crudProduct", `"floating":true,"maxMachines":3`),
		k.license("crudLicense", "crudPolicy", ""),
		k.machine("create full", "crudMachine", "crudLicense", "crud-a-{{run}}",
			`"name":"Office","ip":"10.0.0.1","hostname":"office","platform":"linux","cores":4,"memory":8192,"disk":1024,"metadata":{"seat":1}`,
			"", admin),
		k.machine("create duplicate fingerprint", "", "crudLicense", "crud-a-{{run}}", "", "", admin),
		k.machine("create blank fingerprint", "", "crudLicense", "", "", "", admin),
		k.machine("create fingerprint of another machine's id", "", "crudLicense", "{{crudMachine}}", "", "", admin),
		k.machine("create zero cores", "", "crudLicense", "crud-b-{{run}}", `"cores":0`, "", admin),
		k.machine("create negative memory", "", "crudLicense", "crud-b-{{run}}", `"memory":-1`, "", admin),
		k.machine("create maxProcesses ignored", "", "crudLicense", "crud-b-{{run}}", `"maxProcesses":5`, "", admin),
		k.step("create missing license", http.MethodPost, "/machines",
			`{"data":{"type":"machine","attributes":{"fingerprint":"crud-c-{{run}}"}}}`, admin),
		k.step("create unknown license", http.MethodPost, "/machines",
			`{"data":{"type":"machine","attributes":{"fingerprint":"crud-c-{{run}}"},"relationships":{"license":{"data":{"type":"license","id":"00000000-0000-4000-8000-000000000000"}}}}}`, admin),
		k.step("show by id", http.MethodGet, "/machines/{{crudMachine}}", "", admin),
		k.step("show by fingerprint", http.MethodGet, "/machines/crud-a-{{run}}", "", admin),
		k.step("show missing", http.MethodGet, "/machines/missing-{{run}}", "", admin),
		k.step("update", http.MethodPatch, "/machines/{{crudMachine}}",
			`{"data":{"type":"machine","attributes":{"name":"Home","cores":2,"metadata":{"seat":2},"maxProcesses":3}}}`, admin),
		k.step("update fingerprint ignored", http.MethodPatch, "/machines/{{crudMachine}}",
			`{"data":{"type":"machine","attributes":{"fingerprint":"changed-{{run}}"}}}`, admin),
		k.step("update by fingerprint", http.MethodPatch, "/machines/crud-a-{{run}}",
			`{"data":{"type":"machine","attributes":{"ip":null}}}`, admin),
		k.step("index by license", http.MethodGet, "/machines?license={{crudLicense}}", "", admin),
		k.step("index by fingerprint", http.MethodGet, "/machines?fingerprint=crud-a-{{run}}", "", admin),
		k.step("index by status", http.MethodGet, "/machines?license={{crudLicense}}&status=ALIVE", "", admin),
		k.step("index by hostname", http.MethodGet, "/machines?license={{crudLicense}}&hostname=office", "", admin),
		k.step("license counters", http.MethodGet, "/licenses/{{crudLicense}}", "", admin),
		k.step("license machines", http.MethodGet, "/licenses/{{crudLicense}}/machines", "", admin),
		k.step("destroy", http.MethodDelete, "/machines/{{crudMachine}}", "", admin),
		k.step("show destroyed", http.MethodGet, "/machines/{{crudMachine}}", "", admin),
		k.step("license counters after destroy", http.MethodGet, "/licenses/{{crudLicense}}", "", admin),
	}}
}

func machineLimitsScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_limits", Steps: []conformance.Step{
		k.product("limitProduct"),
		k.policy("strictPolicy", "limitProduct", `"floating":true,"maxMachines":1,"overageStrategy":"NO_OVERAGE"`),
		k.license("strictLicense", "strictPolicy", ""),
		k.machine("first within limit", "", "strictLicense", "strict-1-{{run}}", "", "", admin),
		k.machine("second over limit", "", "strictLicense", "strict-2-{{run}}", "", "", admin),
		k.policy("overagePolicy", "limitProduct", `"floating":true,"maxMachines":2,"overageStrategy":"ALLOW_1_5X_OVERAGE","maxCores":4`),
		k.license("overageLicense", "overagePolicy", ""),
		k.machine("overage 1", "", "overageLicense", "overage-1-{{run}}", `"cores":2`, "", admin),
		k.machine("overage 2", "", "overageLicense", "overage-2-{{run}}", `"cores":2`, "", admin),
		k.machine("overage 3 within 1.5x", "overage3", "overageLicense", "overage-3-{{run}}", `"cores":2`, "", admin),
		k.machine("overage 4 past 1.5x", "", "overageLicense", "overage-4-{{run}}", "", "", admin),
		k.step("update cores past 1.5x", http.MethodPatch, "/machines/{{overage3}}",
			`{"data":{"type":"machine","attributes":{"cores":5}}}`, admin),
		k.policy("alwaysPolicy", "limitProduct", `"floating":true,"maxMachines":1,"overageStrategy":"ALWAYS_ALLOW_OVERAGE","maxMemory":10,"maxDisk":10`),
		k.license("alwaysLicense", "alwaysPolicy", ""),
		k.machine("always 1", "", "alwaysLicense", "always-1-{{run}}", `"memory":100,"disk":100`, "", admin),
		k.machine("always 2", "", "alwaysLicense", "always-2-{{run}}", `"memory":100,"disk":100`, "", admin),
		k.policy("resourcePolicy", "limitProduct", `"floating":true,"maxMachines":5,"maxMemory":10,"maxDisk":10`),
		k.license("resourceLicense", "resourcePolicy", ""),
		k.machine("memory past limit", "", "resourceLicense", "resource-1-{{run}}", `"memory":11`, "", admin),
		k.machine("disk past limit", "", "resourceLicense", "resource-2-{{run}}", `"disk":11`, "", admin),
		k.machine("memory at limit", "", "resourceLicense", "resource-3-{{run}}", `"memory":10,"disk":10`, "", admin),
		k.license("zeroLicense", "strictPolicy", ""),
		k.step("license max machines zero", http.MethodPatch, "/licenses/{{zeroLicense}}",
			`{"data":{"type":"license","attributes":{"maxMachines":0}}}`, admin),
		k.machine("first activation past zero", "", "zeroLicense", "zero-1-{{run}}", "", "", admin),
		k.machine("second activation past zero", "", "zeroLicense", "zero-2-{{run}}", "", "", admin),
	}}
}

func machineLeasingScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_leasing", Steps: []conformance.Step{
		k.product("leaseProduct"),
		k.policy("leasePolicy", "leaseProduct", `"floating":true,"maxMachines":1,"machineLeasingStrategy":"PER_USER"`),
		k.user("leaseOwner"),
		k.user("leaseLicensee"),
		k.user("leaseStranger"),
		k.license("leaseLicense", "leasePolicy", "leaseOwner"),
		k.step("attach licensee", http.MethodPost, "/licenses/{{leaseLicense}}/users",
			`{"data":[{"type":"user","id":"{{leaseLicensee}}"}]}`, admin),
		k.machine("owner first", "", "leaseLicense", "lease-1-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{leaseOwner}}"}}`, admin),
		k.machine("owner second", "", "leaseLicense", "lease-2-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{leaseOwner}}"}}`, admin),
		k.machine("licensee first", "", "leaseLicense", "lease-3-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{leaseLicensee}}"}}`, admin),
		k.machine("unowned first", "", "leaseLicense", "lease-4-{{run}}", "", "", admin),
		k.machine("unowned second", "", "leaseLicense", "lease-5-{{run}}", "", "", admin),
		k.machine("stranger owner", "", "leaseLicense", "lease-6-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{leaseStranger}}"}}`, admin),
		k.machine("unknown owner", "", "leaseLicense", "lease-7-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"00000000-0000-4000-8000-000000000000"}}`, admin),
	}}
}

func machineUniquenessScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	steps := []conformance.Step{k.product("uniqProduct"), k.product("uniqOtherProduct")}

	for _, strategy := range []string{"UNIQUE_PER_ACCOUNT", "UNIQUE_PER_PRODUCT", "UNIQUE_PER_POLICY", "UNIQUE_PER_LICENSE"} {
		attrs := `"floating":true,"maxMachines":5,"machineUniquenessStrategy":"` + strategy + `"`
		steps = append(steps,
			k.policy("p"+strategy, "uniqProduct", attrs),
			k.policy("q"+strategy, "uniqProduct", attrs),
			k.policy("o"+strategy, "uniqOtherProduct", attrs),
			k.license("a"+strategy, "p"+strategy, ""),
			k.license("b"+strategy, "p"+strategy, ""),
			k.license("c"+strategy, "q"+strategy, ""),
			k.license("d"+strategy, "o"+strategy, ""),
			k.machine(strategy+" first", "", "a"+strategy, strategy+"-{{run}}", "", "", admin),
			k.machine(strategy+" same license", "", "a"+strategy, strategy+"-{{run}}", "", "", admin),
			k.machine(strategy+" same policy", "", "b"+strategy, strategy+"-{{run}}", "", "", admin),
			k.machine(strategy+" same product", "", "c"+strategy, strategy+"-{{run}}", "", "", admin),
			k.machine(strategy+" other product", "", "d"+strategy, strategy+"-{{run}}", "", "", admin),
		)
	}

	steps = append(steps,
		k.step("show ambiguous fingerprint", http.MethodGet, "/machines/UNIQUE_PER_LICENSE-{{run}}", "", admin))

	return conformance.Scenario{Name: "machines_uniqueness", Steps: steps}
}

func machineRolesScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	productBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "roleProductToken"}
	ownerBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "roleOwnerToken"}
	licenseeBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "roleLicenseeToken"}
	strangerBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "roleStrangerToken"}
	licenseBearer := conformance.Auth{Kind: conformance.AuthLicenseVar, Var: "roleLicenseKey"}

	steps := []conformance.Step{
		k.product("roleProduct"),
		k.product("roleForeignProduct"),
		k.productToken("roleProductToken", "roleProduct"),
		k.policy("rolePolicy", "roleProduct", `"floating":true,"maxMachines":10,"authenticationStrategy":"MIXED","protected":false`),
		k.policy("roleProtectedPolicy", "roleProduct", `"floating":true,"maxMachines":10,"protected":true`),
		k.policy("roleForeignPolicy", "roleForeignProduct", `"floating":true,"maxMachines":10`),
		k.user("roleOwner"),
		k.user("roleLicensee"),
		k.user("roleStranger"),
		k.userToken("roleOwnerToken", "roleOwner"),
		k.userToken("roleLicenseeToken", "roleLicensee"),
		k.userToken("roleStrangerToken", "roleStranger"),
		k.license("roleLicense", "rolePolicy", "roleOwner"),
		k.license("roleProtectedLicense", "roleProtectedPolicy", "roleOwner"),
		k.license("roleForeignLicense", "roleForeignPolicy", ""),
		k.step("attach licensee", http.MethodPost, "/licenses/{{roleLicense}}/users",
			`{"data":[{"type":"user","id":"{{roleLicensee}}"}]}`, admin),
		{
			Name: "license key of role license", Method: http.MethodGet, Path: k.base + "/licenses/{{roleLicense}}",
			Auth: admin, Capture: map[string]string{"roleLicenseKey": "data.attributes.key"},
		},
		k.machine("owner activates own license", "roleOwnerMachine", "roleLicense", "role-owner-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{roleOwner}}"}}`, ownerBearer),
		k.machine("licensee activates", "roleLicenseeMachine", "roleLicense", "role-licensee-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{roleLicensee}}"}}`, licenseeBearer),
		k.machine("licensee activates unowned", "", "roleLicense", "role-licensee-2-{{run}}", "", "", licenseeBearer),
		k.machine("stranger activates", "", "roleLicense", "role-stranger-{{run}}", "", "", strangerBearer),
		k.machine("owner activates protected", "", "roleProtectedLicense", "role-protected-{{run}}", "", "", ownerBearer),
		k.machine("product activates own", "roleProductMachine", "roleLicense", "role-product-{{run}}", "", "", productBearer),
		k.machine("product activates foreign", "", "roleForeignLicense", "role-foreign-{{run}}", "", "", productBearer),
		k.machine("license activates itself", "roleLicenseMachine", "roleLicense", "role-license-{{run}}", "", "", licenseBearer),
		k.machine("license activates another", "", "roleForeignLicense", "role-license-2-{{run}}", "", "", licenseBearer),
		k.machine("product group relationship", "", "roleLicense", "role-group-{{run}}", "",
			`"group":{"data":{"type":"group","id":"00000000-0000-4000-8000-000000000000"}}`, ownerBearer),
		k.machine("admin activates protected", "roleProtectedMachine", "roleProtectedLicense", "role-protected-admin-{{run}}", "",
			`"owner":{"data":{"type":"user","id":"{{roleOwner}}"}}`, admin),
		k.machine("admin activates foreign", "roleForeignMachine", "roleForeignLicense", "role-foreign-admin-{{run}}", "", "", admin),
	}

	type probe struct {
		who  string
		auth conformance.Auth
	}

	probes := []probe{
		{"product", productBearer},
		{"owner", ownerBearer},
		{"licensee", licenseeBearer},
		{"stranger", strangerBearer},
		{"license", licenseBearer},
	}
	for _, p := range probes {
		steps = append(steps,
			k.step(p.who+" index", http.MethodGet, "/machines?license={{roleLicense}}", "", p.auth),
			k.step(p.who+" index widened", http.MethodGet, "/machines?license={{roleForeignLicense}}", "", p.auth),
			k.step(p.who+" index by foreign product", http.MethodGet, "/machines?product={{roleForeignProduct}}", "", p.auth),
			k.step(p.who+" show owner machine", http.MethodGet, "/machines/{{roleOwnerMachine}}", "", p.auth),
			k.step(p.who+" show foreign machine", http.MethodGet, "/machines/{{roleForeignMachine}}", "", p.auth),
			k.step(p.who+" update licensee machine", http.MethodPatch, "/machines/{{roleLicenseeMachine}}",
				`{"data":{"type":"machine","attributes":{"name":"by `+p.who+`","cores":2}}}`, p.auth),
			k.step(p.who+" update protected machine", http.MethodPatch, "/machines/{{roleProtectedMachine}}",
				`{"data":{"type":"machine","attributes":{"name":"by `+p.who+`"}}}`, p.auth),
			k.step(p.who+" ping owner machine", http.MethodPost, "/machines/{{roleOwnerMachine}}/actions/ping", "", p.auth),
			k.step(p.who+" ping protected machine", http.MethodPost, "/machines/{{roleProtectedMachine}}/actions/ping", "", p.auth),
			k.step(p.who+" reset owner machine", http.MethodPost, "/machines/{{roleOwnerMachine}}/actions/reset", "", p.auth),
			k.step(p.who+" check out licensee machine", http.MethodPost,
				"/machines/{{roleLicenseeMachine}}/actions/check-out?encrypt=false&include=license", "", p.auth),
			k.step(p.who+" check out protected machine", http.MethodPost,
				"/machines/{{roleProtectedMachine}}/actions/check-out", "", p.auth),
			k.step(p.who+" destroy foreign machine", http.MethodDelete, "/machines/{{roleForeignMachine}}", "", p.auth),
			k.step(p.who+" destroy protected machine", http.MethodDelete, "/machines/{{roleProtectedMachine}}", "", p.auth),
		)
	}

	steps = append(steps,
		k.step("licensee destroys own machine", http.MethodDelete, "/machines/{{roleLicenseeMachine}}", "", licenseeBearer),
		k.step("license destroys its machine", http.MethodDelete, "/machines/{{roleLicenseMachine}}", "", licenseBearer),
		k.step("product destroys its machine", http.MethodDelete, "/machines/{{roleProductMachine}}", "", productBearer),
		k.step("owner destroys own machine", http.MethodDelete, "/machines/{{roleOwnerMachine}}", "", ownerBearer),
	)

	return conformance.Scenario{Name: "machines_roles", Steps: steps}
}

func machineHeartbeatScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_heartbeat", Steps: []conformance.Step{
		k.product("beatProduct"),
		k.policy("beatPolicy", "beatProduct",
			`"floating":true,"maxMachines":5,"requireHeartbeat":true,"heartbeatDuration":3600,"heartbeatResurrectionStrategy":"ALWAYS_REVIVE","heartbeatCullStrategy":"KEEP_DEAD"`),
		k.policy("beatCreationPolicy", "beatProduct",
			`"floating":true,"maxMachines":5,"requireHeartbeat":true,"heartbeatDuration":3600,"heartbeatBasis":"FROM_CREATION"`),
		k.policy("beatOptionalPolicy", "beatProduct", `"floating":true,"maxMachines":5`),
		k.license("beatLicense", "beatPolicy", ""),
		k.license("beatCreationLicense", "beatCreationPolicy", ""),
		k.license("beatOptionalLicense", "beatOptionalPolicy", ""),
		k.machine("activate", "beatMachine", "beatLicense", "beat-{{run}}", "", "", admin),
		k.machine("activate from creation", "", "beatCreationLicense", "beat-creation-{{run}}", "", "", admin),
		k.machine("activate optional", "beatOptional", "beatOptionalLicense", "beat-optional-{{run}}", "", "", admin),
		k.step("ping", http.MethodPost, "/machines/{{beatMachine}}/actions/ping", "", admin),
		k.step("ping heartbeat alias", http.MethodPost, "/machines/{{beatMachine}}/actions/ping-heartbeat", "", admin),
		k.step("show alive", http.MethodGet, "/machines/{{beatMachine}}", "", admin),
		k.step("index alive", http.MethodGet, "/machines?license={{beatLicense}}&status=ALIVE", "", admin),
		k.step("index dead", http.MethodGet, "/machines?license={{beatLicense}}&status=DEAD", "", admin),
		k.step("index unknown status", http.MethodGet, "/machines?license={{beatLicense}}&status=NOPE", "", admin),
		k.step("reset", http.MethodPost, "/machines/{{beatMachine}}/actions/reset", "", admin),
		k.step("reset heartbeat alias", http.MethodPost, "/machines/{{beatMachine}}/actions/reset-heartbeat", "", admin),
		k.step("ping optional", http.MethodPost, "/machines/{{beatOptional}}/actions/ping", "", admin),
		k.step("show optional alive", http.MethodGet, "/machines/{{beatOptional}}", "", admin),
		k.step("ping missing", http.MethodPost, "/machines/missing-{{run}}/actions/ping", "", admin),
	}}
}

func machineCheckoutScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_checkout", Steps: []conformance.Step{
		k.product("outProduct"),
		k.policy("outPolicy", "outProduct", `"floating":true,"maxMachines":5,"scheme":"ED25519_SIGN"`),
		k.license("outLicense", "outPolicy", ""),
		k.machine("activate", "outMachine", "outLicense", "out-{{run}}", "", "", admin),
		k.step("check out default", http.MethodPost, "/machines/{{outMachine}}/actions/check-out", "", admin),
		k.step("check out by fingerprint", http.MethodPost, "/machines/out-{{run}}/actions/check-out", "", admin),
		k.step("check out unencrypted with includes", http.MethodPost,
			"/machines/{{outMachine}}/actions/check-out?encrypt=false&include=license,license.entitlements,license.product,license.policy,license.owner,owner,group,components",
			"", admin),
		k.step("check out encrypted", http.MethodPost, "/machines/{{outMachine}}/actions/check-out?encrypt=true", "", admin),
		k.step("check out ttl", http.MethodPost, "/machines/{{outMachine}}/actions/check-out?ttl=86400", "", admin),
		k.step("check out ttl too short", http.MethodPost, "/machines/{{outMachine}}/actions/check-out?ttl=60", "", admin),
		k.step("check out ttl null", http.MethodPost, "/machines/{{outMachine}}/actions/check-out",
			`{"meta":{"ttl":null}}`, admin),
		k.step("check out unknown include", http.MethodPost, "/machines/{{outMachine}}/actions/check-out?include=nope", "", admin),
		k.step("check out meta", http.MethodPost, "/machines/{{outMachine}}/actions/check-out",
			`{"meta":{"encrypt":false,"include":["license"],"ttl":3600}}`, admin),
		{
			Name: "check out raw", Method: http.MethodGet, Path: k.base + "/machines/{{outMachine}}/actions/check-out",
			Auth: admin, Headers: map[string]string{"Accept": "application/octet-stream"},
		},
		k.step("show last check out", http.MethodGet, "/machines/{{outMachine}}", "", admin),
		k.step("generate offline proof", http.MethodPost, "/machines/{{outMachine}}/actions/generate-offline-proof", "", admin),
	}}
}

func machineRelationshipsScenario(k machineKit) conformance.Scenario {
	admin := k.admin

	return conformance.Scenario{Name: "machines_relationships", Steps: []conformance.Step{
		k.product("relProduct"),
		k.policy("relPolicy", "relProduct", `"floating":true,"maxMachines":5`),
		k.user("relOwner"),
		k.user("relLicensee"),
		k.user("relStranger"),
		k.create("group relLimited", "/groups",
			`{"data":{"type":"groups","attributes":{"name":"machines-limited-{{run}}","maxMachines":1}}}`, "relLimited"),
		k.create("group relOpen", "/groups",
			`{"data":{"type":"groups","attributes":{"name":"machines-open-{{run}}"}}}`, "relOpen"),
		k.license("relLicense", "relPolicy", "relOwner"),
		k.step("attach licensee", http.MethodPost, "/licenses/{{relLicense}}/users",
			`{"data":[{"type":"user","id":"{{relLicensee}}"}]}`, admin),
		k.machine("activate", "relMachine", "relLicense", "rel-1-{{run}}", "", "", admin),
		k.machine("activate in limited group", "", "relLicense", "rel-2-{{run}}", "",
			`"group":{"data":{"type":"group","id":"{{relLimited}}"}}`, admin),
		k.machine("activate past group limit", "", "relLicense", "rel-3-{{run}}", "",
			`"group":{"data":{"type":"group","id":"{{relLimited}}"}}`, admin),
		k.step("product", http.MethodGet, "/machines/{{relMachine}}/product", "", admin),
		k.step("license", http.MethodGet, "/machines/{{relMachine}}/license", "", admin),
		k.step("owner empty", http.MethodGet, "/machines/{{relMachine}}/owner", "", admin),
		k.step("user alias empty", http.MethodGet, "/machines/{{relMachine}}/user", "", admin),
		k.step("group empty", http.MethodGet, "/machines/{{relMachine}}/group", "", admin),
		k.step("set owner licensee", http.MethodPut, "/machines/{{relMachine}}/owner",
			`{"data":{"type":"user","id":"{{relLicensee}}"}}`, admin),
		k.step("owner", http.MethodGet, "/machines/{{relMachine}}/owner", "", admin),
		k.step("set owner stranger", http.MethodPut, "/machines/{{relMachine}}/owner",
			`{"data":{"type":"user","id":"{{relStranger}}"}}`, admin),
		k.step("set owner null", http.MethodPut, "/machines/{{relMachine}}/owner", `{"data":null}`, admin),
		k.step("set group open", http.MethodPut, "/machines/{{relMachine}}/group",
			`{"data":{"type":"group","id":"{{relOpen}}"}}`, admin),
		k.step("group", http.MethodGet, "/machines/{{relMachine}}/group", "", admin),
		k.step("set group past limit", http.MethodPut, "/machines/{{relMachine}}/group",
			`{"data":{"type":"group","id":"{{relLimited}}"}}`, admin),
		k.step("set group unknown", http.MethodPut, "/machines/{{relMachine}}/group",
			`{"data":{"type":"group","id":"00000000-0000-4000-8000-000000000000"}}`, admin),
		k.step("set group null", http.MethodPut, "/machines/{{relMachine}}/group", `{"data":null}`, admin),
		k.step("components empty", http.MethodGet, "/machines/{{relMachine}}/components", "", admin),
		k.step("processes empty", http.MethodGet, "/machines/{{relMachine}}/processes", "", admin),
		k.step("user machines", http.MethodGet, "/users/{{relLicensee}}/machines", "", admin),
		k.step("license machine by fingerprint", http.MethodGet, "/licenses/{{relLicense}}/machines/rel-1-{{run}}", "", admin),
	}}
}

func machineActivationTokenScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	activation := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "actToken"}

	return conformance.Scenario{Name: "machines_activation_tokens", Steps: []conformance.Step{
		k.product("actProduct"),
		k.policy("actPolicy", "actProduct", `"floating":true,"maxMachines":5`),
		k.license("actLicense", "actPolicy", ""),
		{
			Name: "activation token", Method: http.MethodPost, Path: k.base + "/licenses/{{actLicense}}/tokens",
			Auth: admin, Body: `{"data":{"type":"tokens","attributes":{"maxActivations":1,"maxDeactivations":1}}}`,
			Capture: map[string]string{"actToken": "data.attributes.token", "actTokenID": "data.id"},
		},
		k.machine("activate with token", "actMachine", "actLicense", "act-1-{{run}}", "", "", activation),
		k.machine("activate past max activations", "", "actLicense", "act-2-{{run}}", "", "", activation),
		k.machine("activate invalid with token", "", "actLicense", "", "", "", activation),
		k.step("token counters", http.MethodGet, "/tokens/{{actTokenID}}", "", admin),
		k.machine("admin activates", "actMachine2", "actLicense", "act-3-{{run}}", "", "", admin),
		k.step("deactivate with token", http.MethodDelete, "/machines/{{actMachine}}", "", activation),
		k.step("deactivate past max deactivations", http.MethodDelete, "/machines/{{actMachine2}}", "", activation),
		k.step("token counters after", http.MethodGet, "/tokens/{{actTokenID}}", "", admin),
	}}
}
