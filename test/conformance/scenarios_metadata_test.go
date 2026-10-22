package conformance_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

func metadataScenarios() []conformance.Scenario {
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + conformance.AccountSlug
	tooMany := make(map[string]any)

	for index := range 65 {
		tooMany[fmt.Sprintf("k%d", index)] = index
	}

	tooMany["blob"] = strings.Repeat("x", 17000)
	probes := []struct {
		name     string
		metadata map[string]any
	}{
		{"exact_bytes", map[string]any{"a": strings.Repeat("x", 16376)}},
		{"over_bytes", map[string]any{"a": strings.Repeat("x", 16377)}},
		{"html_bytes", map[string]any{"a": strings.Repeat("<>&", 5000)}},
		{"depth_four", map[string]any{"a": []any{map[string]any{"b": []any{1}}}}},
		{"depth_five", map[string]any{"a": []any{map[string]any{"b": []any{[]any{1}}}}}},
		{"multiple_limits", tooMany},
	}
	out := make([]conformance.Scenario, 0, 6)

	for _, resource := range []string{"products", "policies", "licenses", "users", "groups", "entitlements"} {
		steps := make([]conformance.Step, 0, len(probes)+2)
		if resource == "policies" || resource == "licenses" {
			steps = append(steps, conformance.Step{
				Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
				Body:    `{"data":{"type":"product","attributes":{"name":"metadata-product-{{run}}"}}}`,
				Capture: map[string]string{"product": "data.id"},
			})
		}

		if resource == "licenses" {
			steps = append(steps, conformance.Step{
				Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
				Body:    `{"data":{"type":"policy","attributes":{"name":"metadata-policy-{{run}}"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
				Capture: map[string]string{"policy": "data.id"},
			})
		}

		for _, probe := range probes {
			attributes := map[string]any{
				"name": "metadata-" + probe.name + "-{{run}}", "metadata": probe.metadata,
			}
			data := map[string]any{"type": resource, "attributes": attributes}

			switch resource {
			case "users":
				delete(attributes, "name")
				attributes["email"] = "metadata-" + probe.name + "-{{run}}@example.com"
			case "entitlements":
				attributes["code"] = "metadata-" + probe.name + "-{{run}}"
			case "policies":
				data["relationships"] = metadataRelationship("product", "{{product}}")
			case "licenses":
				data["relationships"] = metadataRelationship("policy", "{{policy}}")
			}

			body, err := json.Marshal(map[string]any{"data": data})
			if err != nil {
				panic(err) // only static JSON-compatible test data
			}

			steps = append(steps, conformance.Step{
				Name: probe.name, Method: http.MethodPost, Path: base + "/" + resource,
				Auth: admin, Body: string(body),
			})
		}

		out = append(out, conformance.Scenario{Name: "metadata_" + resource, Steps: steps})
	}

	return out
}

func metadataRelationship(resource, id string) map[string]any {
	return map[string]any{resource: map[string]any{"data": map[string]any{"type": resource, "id": id}}}
}
