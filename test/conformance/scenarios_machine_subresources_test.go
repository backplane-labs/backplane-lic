package conformance_test

import (
	"net/http"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func machineSubresourceScenarios() []conformance.Scenario {
	k := newMachineKit()

	return []conformance.Scenario{componentsScenario(k), componentUniquenessScenario(k), processesScenario(k)}
}

func component(fingerprint, name string) string {
	return `{"type":"component","attributes":{"fingerprint":"` + fingerprint + `","name":"` + name + `"}}`
}

func componentsScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	owner := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "compOwnerToken"}
	stranger := conformance.Auth{Kind: conformance.AuthTokenVar, Var: "compStrangerToken"}
	createComponent := func(name, capture, machine, body string, auth conformance.Auth) conformance.Step {
		step := k.step(name, http.MethodPost, "/components",
			`{"data":{"type":"component","attributes":`+body+
				`,"relationships":{"machine":{"data":{"type":"machine","id":"{{`+machine+`}}"}}}}}`, auth)
		if capture != "" {
			step.Capture = map[string]string{capture: "data.id"}
		}

		return step
	}

	return conformance.Scenario{Name: "machines_components", Steps: []conformance.Step{
		k.product("compProduct"),
		k.policy("compPolicy", "compProduct", `"floating":true,"maxMachines":10,"protected":false`),
		k.user("compOwner"),
		k.user("compStranger"),
		k.userToken("compOwnerToken", "compOwner"),
		k.userToken("compStrangerToken", "compStranger"),
		k.license("compLicense", "compPolicy", "compOwner"),
		k.machine("activate with components", "compMachine", "compLicense", "comp-1-{{run}}", "",
			`"components":{"data":[`+component("cpu-{{run}}", "CPU")+`,`+component("disk-{{run}}", "Disk")+`]}`, admin),
		k.machine("activate with duplicated components", "", "compLicense", "comp-2-{{run}}", "",
			`"components":{"data":[`+component("dup-{{run}}", "A")+`,`+component("dup-{{run}}", "B")+`]}`, admin),
		k.machine("activate with a blank component name", "", "compLicense", "comp-3-{{run}}", "",
			`"components":{"data":[`+component("blank-{{run}}", "")+`]}`, admin),
		k.step("machine components", http.MethodGet, "/machines/{{compMachine}}/components", "", admin),
		createComponent("create", "compComponent", "compMachine", `{"fingerprint":"gpu-{{run}}","name":"GPU","metadata":{"slot":1}}`, admin),
		createComponent("create duplicate fingerprint", "", "compMachine", `{"fingerprint":"gpu-{{run}}","name":"GPU"}`, admin),
		createComponent("create reserved fingerprint", "", "compMachine", `{"fingerprint":"actions","name":"GPU"}`, admin),
		createComponent("create blank name", "", "compMachine", `{"fingerprint":"nic-{{run}}","name":""}`, admin),
		createComponent("create missing name", "", "compMachine", `{"fingerprint":"nic-{{run}}"}`, admin),
		k.step("create unknown machine", http.MethodPost, "/components",
			`{"data":{"type":"component","attributes":{"fingerprint":"x-{{run}}","name":"X"},"relationships":{"machine":{"data":{"type":"machine","id":"00000000-0000-4000-8000-000000000000"}}}}}`, admin),
		k.step("show", http.MethodGet, "/components/{{compComponent}}", "", admin),
		k.step("show missing", http.MethodGet, "/components/00000000-0000-4000-8000-000000000000", "", admin),
		k.step("show not a uuid", http.MethodGet, "/components/nope", "", admin),
		k.step("index by machine", http.MethodGet, "/components?machine={{compMachine}}", "", admin),
		k.step("index by license", http.MethodGet, "/components?license={{compLicense}}", "", admin),
		k.step("update", http.MethodPatch, "/components/{{compComponent}}",
			`{"data":{"type":"component","attributes":{"name":"Graphics","metadata":{"slot":2}}}}`, admin),
		k.step("update fingerprint", http.MethodPatch, "/components/{{compComponent}}",
			`{"data":{"type":"component","attributes":{"fingerprint":"changed-{{run}}"}}}`, admin),
		k.step("machine relationship", http.MethodGet, "/components/{{compComponent}}/machine", "", admin),
		k.step("license relationship", http.MethodGet, "/components/{{compComponent}}/license", "", admin),
		k.step("product relationship", http.MethodGet, "/components/{{compComponent}}/product", "", admin),
		k.step("machine component", http.MethodGet, "/machines/{{compMachine}}/components/{{compComponent}}", "", admin),
		k.step("owner index", http.MethodGet, "/components?machine={{compMachine}}", "", owner),
		k.step("owner show", http.MethodGet, "/components/{{compComponent}}", "", owner),
		k.step("owner update metadata", http.MethodPatch, "/components/{{compComponent}}",
			`{"data":{"type":"component","attributes":{"metadata":{"slot":3}}}}`, owner),
		k.step("owner update name", http.MethodPatch, "/components/{{compComponent}}",
			`{"data":{"type":"component","attributes":{"name":"Owner GPU"}}}`, owner),
		createComponent("owner create", "", "compMachine", `{"fingerprint":"owner-{{run}}","name":"Owner"}`, owner),
		k.step("stranger show", http.MethodGet, "/components/{{compComponent}}", "", stranger),
		k.step("stranger index", http.MethodGet, "/components?machine={{compMachine}}", "", stranger),
		createComponent("stranger create", "", "compMachine", `{"fingerprint":"stranger-{{run}}","name":"S"}`, stranger),
		k.step("stranger destroy", http.MethodDelete, "/components/{{compComponent}}", "", stranger),
		k.step("owner destroy", http.MethodDelete, "/components/{{compComponent}}", "", owner),
		k.step("show destroyed", http.MethodGet, "/components/{{compComponent}}", "", admin),
	}}
}

func componentUniquenessScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	steps := []conformance.Step{k.product("cuProduct")}

	for _, strategy := range []string{"UNIQUE_PER_LICENSE", "UNIQUE_PER_POLICY"} {
		steps = append(steps,
			k.policy("cu"+strategy, "cuProduct", `"floating":true,"maxMachines":5,"componentUniquenessStrategy":"`+strategy+`"`),
			k.license("cuA"+strategy, "cu"+strategy, ""),
			k.license("cuB"+strategy, "cu"+strategy, ""),
			k.machine(strategy+" first", "", "cuA"+strategy, strategy+"-1-{{run}}", "",
				`"components":{"data":[`+component(strategy+"-c-{{run}}", "C")+`]}`, admin),
			k.machine(strategy+" same license", "", "cuA"+strategy, strategy+"-2-{{run}}", "",
				`"components":{"data":[`+component(strategy+"-c-{{run}}", "C")+`]}`, admin),
			k.machine(strategy+" same policy", "", "cuB"+strategy, strategy+"-3-{{run}}", "",
				`"components":{"data":[`+component(strategy+"-c-{{run}}", "C")+`]}`, admin),
		)
	}

	return conformance.Scenario{Name: "machines_component_uniqueness", Steps: steps}
}

func processesScenario(k machineKit) conformance.Scenario {
	admin := k.admin
	spawn := func(name, capture, machine, pid string) conformance.Step {
		step := k.step(name, http.MethodPost, "/processes",
			`{"data":{"type":"process","attributes":{"pid":"`+pid+
				`"},"relationships":{"machine":{"data":{"type":"machine","id":"{{`+machine+`}}"}}}}}`, admin)
		if capture != "" {
			step.Capture = map[string]string{capture: "data.id"}
		}

		return step
	}

	return conformance.Scenario{Name: "machines_processes", Steps: []conformance.Step{
		k.product("procProduct"),
		k.policy("procMachinePolicy", "procProduct", `"floating":true,"maxMachines":5,"maxProcesses":1,"heartbeatDuration":3600`),
		k.policy("procLicensePolicy", "procProduct",
			`"floating":true,"maxMachines":5,"maxProcesses":2,"processLeasingStrategy":"PER_LICENSE","overageStrategy":"NO_OVERAGE"`),
		k.license("procLicense", "procMachinePolicy", ""),
		k.license("procPooled", "procLicensePolicy", ""),
		k.machine("activate", "procMachine", "procLicense", "proc-1-{{run}}", "", "", admin),
		k.machine("activate pooled a", "procPooledA", "procPooled", "proc-2-{{run}}", "", "", admin),
		k.machine("activate pooled b", "procPooledB", "procPooled", "proc-3-{{run}}", "", "", admin),
		spawn("spawn", "procProcess", "procMachine", "1001"),
		spawn("spawn past machine limit", "", "procMachine", "1002"),
		spawn("spawn duplicate pid", "", "procMachine", "1001"),
		spawn("spawn reserved pid", "", "procMachine", "actions"),
		spawn("spawn pooled 1", "", "procPooledA", "1"),
		spawn("spawn pooled 2", "", "procPooledB", "2"),
		spawn("spawn pooled past license limit", "", "procPooledB", "3"),
		k.step("spawn with id", http.MethodPost, "/processes",
			`{"data":{"type":"process","id":"not-a-uuid","attributes":{"pid":"9"},"relationships":{"machine":{"data":{"type":"machine","id":"{{procMachine}}"}}}}}`, admin),
		k.step("spawn unknown machine", http.MethodPost, "/processes",
			`{"data":{"type":"process","attributes":{"pid":"9"},"relationships":{"machine":{"data":{"type":"machine","id":"00000000-0000-4000-8000-000000000000"}}}}}`, admin),
		k.step("show", http.MethodGet, "/processes/{{procProcess}}", "", admin),
		k.step("ping", http.MethodPost, "/processes/{{procProcess}}/actions/ping", "", admin),
		k.step("index alive", http.MethodGet, "/processes?machine={{procMachine}}&status=alive", "", admin),
		k.step("index dead", http.MethodGet, "/processes?machine={{procMachine}}&status=DEAD", "", admin),
		k.step("index by license", http.MethodGet, "/processes?license={{procPooled}}", "", admin),
		k.step("machine processes", http.MethodGet, "/machines/{{procMachine}}/processes", "", admin),
		k.step("machine process", http.MethodGet, "/machines/{{procMachine}}/processes/{{procProcess}}", "", admin),
		k.step("update metadata", http.MethodPatch, "/processes/{{procProcess}}",
			`{"data":{"type":"process","attributes":{"metadata":{"worker":true}}}}`, admin),
		k.step("update pid", http.MethodPatch, "/processes/{{procProcess}}",
			`{"data":{"type":"process","attributes":{"pid":"2002"}}}`, admin),
		k.step("machine relationship", http.MethodGet, "/processes/{{procProcess}}/machine", "", admin),
		k.step("license relationship", http.MethodGet, "/processes/{{procProcess}}/license", "", admin),
		k.step("product relationship", http.MethodGet, "/processes/{{procProcess}}/product", "", admin),
		k.step("kill", http.MethodDelete, "/processes/{{procProcess}}", "", admin),
		k.step("show killed", http.MethodGet, "/processes/{{procProcess}}", "", admin),
		spawn("spawn after kill", "", "procMachine", "1003"),
		k.step("deactivate machine", http.MethodDelete, "/machines/{{procMachine}}", "", admin),
		k.step("processes of deactivated machine", http.MethodGet, "/processes?machine={{procMachine}}", "", admin),
	}}
}
