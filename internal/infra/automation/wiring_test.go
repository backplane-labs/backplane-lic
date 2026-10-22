package automation_test

import (
	"bytes"
	"encoding/json"
	htmltemplate "html/template"
	"io"
	"os"
	"path/filepath"
	"testing"
	texttemplate "text/template"

	consolev1 "github.com/backplane-labs/backplane/backplanepb/console/v1"
	"github.com/google/cel-go/cel"
	"go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/backplane-labs/backplane-lic/internal/app/port"
)

// The platform's wiring compiler lives under backplane/internal and is not
// importable by SDK services. Check its public protobuf format and CEL here.
func TestShippedWiring(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile("../../../deployments/wiring/lic.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var file struct {
		Bindings []map[string]any `yaml:"bindings"`
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	if err := decoder.Decode(&file); err != nil {
		t.Fatal(err)
	}

	if len(file.Bindings) != 1 {
		t.Fatalf("bindings = %d", len(file.Bindings))
	}

	encoded, err := json.Marshal(file.Bindings[0])
	if err != nil {
		t.Fatal(err)
	}

	definition := &consolev1.BindingDefinition{}
	if err := protojson.Unmarshal(encoded, definition); err != nil {
		t.Fatal(err)
	}

	if definition.GetHook() != "lic.Deliver" || len(definition.GetSteps()) != 2 {
		t.Fatalf("binding = %s", definition)
	}

	rendered := definition.GetSteps()["rendered"]

	mail := definition.GetSteps()["mail"]
	if rendered.GetActivity() != "templates.Render" || mail.GetActivity() != "courier.SendMail" {
		t.Fatal("incorrect activity targets")
	}

	env, err := cel.NewEnv(cel.Variable("req", cel.DynType), cel.Variable("rendered", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}

	vars := map[string]any{
		"req": map[string]any{
			"template": "lic.password-reset", "locale": "ru", "recipient": "alex@example.com",
			"data": map[string]any{"user_name": "Alex", "reset_url": "https://licenses.example.com/portal/reset?claim=example"},
		},
		"rendered": map[string]any{"parts": map[string]any{"subject": "Reset", "text": "Text", "html": "<p>HTML</p>"}},
	}

	for _, step := range definition.GetSteps() {
		for field, value := range step.GetInput().GetStructValue().GetFields() {
			expressions := []string{value.GetStringValue()}
			if field == "to" {
				expressions = []string{value.GetListValue().GetValues()[0].GetStringValue()}
			}

			for _, expression := range expressions {
				ast, issues := env.Compile(expression)
				if issues.Err() != nil {
					t.Fatalf("%s: %v", expression, issues.Err())
				}

				program, err := env.Program(ast)
				if err != nil {
					t.Fatal(err)
				}

				if _, _, err := program.Eval(vars); err != nil {
					t.Fatalf("%s: %v", expression, err)
				}
			}
		}
	}
}

func TestShippedTemplates(t *testing.T) {
	t.Parallel()

	root := "../../../deployments/templates"
	for _, name := range []string{"lic.password-reset", "lic.license-expiring-soon"} {
		metadata, err := os.ReadFile(filepath.Join(root, name, "template.yaml"))
		if err != nil {
			t.Fatal(err)
		}

		var config struct {
			Description     string         `yaml:"description"`
			DefaultLanguage string         `yaml:"default_language"` //nolint:tagliatelle // template service file format
			Sample          map[string]any `yaml:"sample"`
		}
		if err := yaml.Unmarshal(metadata, &config); err != nil {
			t.Fatal(err)
		}

		for _, locale := range []string{"en", "ru"} {
			for _, part := range []string{"subject.txt", "text.txt", "html.html"} {
				path := filepath.Join(root, name, locale, part)

				source, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}

				var compiled templateExecutor
				if filepath.Ext(part) == ".html" {
					compiled, err = htmltemplate.New(part).Option("missingkey=error").Parse(string(source))
				} else {
					compiled, err = texttemplate.New(part).Option("missingkey=error").Parse(string(source))
				}

				if err != nil {
					t.Fatal(err)
				}

				var rendered bytes.Buffer
				if err := compiled.Execute(&rendered, config.Sample); err != nil {
					t.Fatalf("%s: %v", path, err)
				}

				// The real payload, with every optional brand field empty,
				// must render too: a key the sample has but the payload
				// lacks fails here, not in production.
				if err := compiled.Execute(io.Discard, deliveryPayload(t)); err != nil {
					t.Fatalf("%s with the delivery payload: %v", path, err)
				}

				if rendered.Len() == 0 {
					t.Fatalf("empty template %s", path)
				}
			}
		}
	}
}

func deliveryPayload(t *testing.T) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(port.DeliveryData{BrandName: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}

	return payload
}

type templateExecutor interface {
	Execute(writer io.Writer, data any) error
}
