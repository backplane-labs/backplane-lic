package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// searchMachineScenarios covers POST /search for machines: every searchable
// attribute, AND/OR, pagination and the bearers the search policy admits or
// denies. Terms carry the run suffix so parallel scenarios on the shared
// account never leak into the results.
func searchMachineScenarios() []conformance.Scenario {
	k := newMachineKit()
	productBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "searchProductToken"}
	userBearer := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "searchOwnerToken"}

	query := func(name, body string) conformance.Step {
		return k.step(name, http.MethodPost, "/search", body, k.admin)
	}

	return []conformance.Scenario{{
		Name: "search_machines",
		Steps: []conformance.Step{
			k.product("searchProduct"),
			k.productToken("searchProductToken", "searchProduct"),
			k.policy("searchPolicy", "searchProduct", `"floating":true,"maxMachines":5`),
			k.user("searchOwner"),
			k.userToken("searchOwnerToken", "searchOwner"),
			k.license("searchLicense", "searchPolicy", "searchOwner"),
			k.license("searchOtherLicense", "searchPolicy", ""),
			k.machine("activate first", "searchFirst", "searchLicense", "srchm-first-{{run}}",
				`"name":"Srchm{{run}} Laptop","metadata":{"rack":"r{{run}}","slots":1042}`,
				`"owner":{"data":{"type":"user","id":"{{searchOwner}}"}}`, k.admin),
			k.machine("activate second", "searchSecond", "searchOtherLicense", "srchm-second-{{run}}",
				`"name":"Srchm{{run}} Server","metadata":{"rack":"r{{run}}","slots":7007}`, "", k.admin),
			query("by id", `{"meta":{"type":"machines","query":{"id":"{{searchFirst}}"}}}`),
			query("by singular type", `{"meta":{"type":"machine","query":{"id":"{{searchSecond}}"}}}`),
			query("by fingerprint", `{"meta":{"type":"machines","query":{"fingerprint":"srchm-first-{{run}}"}}}`),
			query("by partial fingerprint", `{"meta":{"type":"machines","query":{"fingerprint":"second-{{run}}"}}}`),
			query("by name", `{"meta":{"type":"machines","query":{"name":"srchm{{run}}"}}}`),
			query("by metadata", `{"meta":{"type":"machines","query":{"metadata":{"rack":"r{{run}}"}}}}`),
			query("by cast metadata", `{"meta":{"type":"machines","query":{"metadata":{"slots":"1042","rack":"r{{run}}"}}}}`),
			query("by license id", `{"meta":{"type":"machines","query":{"license":"{{searchLicense}}"}}}`),
			query("by other license id", `{"meta":{"type":"machines","query":{"license":"{{searchOtherLicense}}"}}}`),
			query("by owner email", `{"meta":{"type":"machines","query":{"owner":"machines-searchOwner-{{run}}@example"}}}`),
			query("by owner id", `{"meta":{"type":"machines","query":{"owner":"{{searchOwner}}"}}}`),
			query("by user email", `{"meta":{"type":"machines","query":{"user":"machines-searchOwner-{{run}}@example"}}}`),
			query("by user id", `{"meta":{"type":"machines","query":{"user":"{{searchOwner}}"}}}`),
			query("by product id", `{"meta":{"type":"machines","query":{"product":"{{searchProduct}}"}}}`),
			query("by product name", `{"meta":{"type":"machines","query":{"product":"machines-searchProduct-{{run}}"}}}`),
			query("by policy id", `{"meta":{"type":"machines","query":{"policy":"{{searchPolicy}}"}}}`),
			query("by policy name", `{"meta":{"type":"machines","query":{"policy":"machines-searchPolicy-{{run}}"}}}`),
			query("and", `{"meta":{"type":"machines","query":{"name":"srchm{{run}}","metadata":{"slots":"7007"}}}}`),
			query("or", `{"meta":{"type":"machines","op":"or","query":{"id":"{{searchFirst}}","fingerprint":"srchm-second-{{run}}"}}}`),
			query("no matches", `{"meta":{"type":"machines","query":{"name":"nothing{{run}}"}}}`),
			query("unsupported attribute", `{"meta":{"type":"machines","query":{"hostname":"abc"}}}`),
			query("too small", `{"meta":{"type":"machines","query":{"fingerprint":"ab"}}}`),
			k.step("paged", http.MethodPost, "/search?page[number]=2&page[size]=1",
				`{"meta":{"type":"machines","query":{"name":"srchm{{run}}"}}}`, k.admin),
			k.step("product bearer is denied", http.MethodPost, "/search",
				`{"meta":{"type":"machines","query":{"name":"srchm{{run}}"}}}`, productBearer),
			k.step("user bearer is denied", http.MethodPost, "/search",
				`{"meta":{"type":"machines","query":{"name":"srchm{{run}}"}}}`, userBearer),
			k.step("anonymous is unauthorized", http.MethodPost, "/search",
				`{"meta":{"type":"machines","query":{"name":"srchm{{run}}"}}}`, conformance.Auth{}),
		},
	}}
}
