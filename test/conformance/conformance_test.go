package conformance_test

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

// record switches the suite into recording mode: scenarios run against the
// Keygen CE reference and write testdata/<scenario>.golden.json.
var record = flag.Bool("record", false, "record golden transcripts from the Keygen CE reference server")

func TestConformance(t *testing.T) {
	t.Parallel()

	scns := scenarios()

	if *record {
		reference := conformance.NewReferenceTarget(referenceURL())
		key := conformance.Ed25519Key(t, reference)

		for _, scenario := range scns {
			t.Run(scenario.Name, func(t *testing.T) {
				t.Parallel()

				transcript := conformance.Run(t, reference, scenario, key)
				golden := filepath.Join("testdata", scenario.Name+".golden.json")

				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatalf("conformance: create testdata: %v", err)
				}

				pretty := normalisedStep(t, transcript, -1) + "\n"

				if err := os.WriteFile(golden, []byte(pretty), 0o644); err != nil {
					t.Fatalf("conformance: write %s: %v", golden, err)
				}
			})
		}

		return
	}

	target := serverUnderTest(t)
	key := conformance.Ed25519Key(t, target)

	for _, scenario := range scns {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()

			golden := loadGolden(t, scenario.Name)
			live := conformance.Run(t, target, scenario, key)

			if dir := os.Getenv("LIC_DEBUG_LIVE"); dir != "" {
				dump := filepath.Join(dir, scenario.Name+".live.json")

				_ = os.WriteFile(dump, []byte(normalisedStep(t, live, -1)), 0o600)
			}

			compare(t, golden, live)
		})
	}
}

// serverUnderTest is the running backplane-lic at LIC_CONFORMANCE_TARGET,
// or one served in-process over the database at LIC_TEST_DATABASE_URL with
// the reference's account and admin.
//
//nolint:ireturn // either target
func serverUnderTest(t *testing.T) conformance.Target {
	t.Helper()

	if url := os.Getenv("LIC_CONFORMANCE_TARGET"); url != "" {
		return conformance.NewTarget(url)
	}

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIC_CONFORMANCE_TARGET or LIC_TEST_DATABASE_URL (make up)")
	}

	ctx := t.Context()

	store, err := postgres.Open(ctx, xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatalf("conformance: open store: %v", err)
	}

	t.Cleanup(store.Close)

	spec := accounts.Spec{ID: uuid.MustParse(conformance.AccountID), Name: "example.com", Slug: conformance.AccountSlug}
	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("conformance: bootstrap: %v", err)
	}

	if err := testkit.SeedAdmin(ctx, store, spec.ID, conformance.AdminEmail, conformance.AdminPass); err != nil {
		t.Fatalf("conformance: seed admin: %v", err)
	}

	handler := service.Handler(store, httpapi.NewMemoryThrottler(), spec, service.HTTP{}, "keygen.test", "conformance")

	return &conformance.HandlerTarget{Handler: handler}
}

func referenceURL() string {
	if url := os.Getenv("LIC_CONFORMANCE_REFERENCE"); url != "" {
		return url
	}

	return conformance.DefaultReferenceURL
}

func loadGolden(t *testing.T, name string) conformance.Transcript {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name+".golden.json"))
	if err != nil {
		t.Fatalf("conformance: read golden (record it with `make conformance-record`): %v", err)
	}

	var tr conformance.Transcript
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("conformance: decode golden: %v", err)
	}

	return tr
}

// compare diffs every step; a scenario passes only when every step matches.
func compare(t *testing.T, golden, live conformance.Transcript) {
	t.Helper()

	if len(golden.Steps) != len(live.Steps) {
		t.Fatalf("conformance: golden has %d steps, live run has %d", len(golden.Steps), len(live.Steps))
	}

	// Placeholders number by first appearance across the whole transcript,
	// as recorded, so both sides are normalised whole.

	// Debug aid: name the first differing step.
	for i := range golden.Steps {
		same := normalisedStep(t, golden, i) == normalisedStep(t, live, i)

		if !same {
			t.Logf("conformance: first differing step %d %q (golden %d, live %d)",
				i, golden.Steps[i].Name, golden.Steps[i].Status, live.Steps[i].Status)

			break
		}
	}

	want := normalisedStep(t, golden, -1)
	got := normalisedStep(t, live, -1)

	if want != got {
		t.Errorf("conformance: transcript differs (-golden +live):\n%s", conformance.Diff(want, got))
	}
}

// normalisedStep renders a normalised pretty-printed JSON view of one step
// (index -1 renders the whole transcript) for golden files and diffs.
func normalisedStep(t *testing.T, transcript conformance.Transcript, index int) string {
	t.Helper()

	var value any

	if index < 0 {
		value = decode(t, transcript)
	} else {
		value = decode(t, transcript.Steps[index])
	}

	normalised := conformance.Normalise(value, transcript.Run)

	pretty, err := json.MarshalIndent(normalised, "", "  ")
	if err != nil {
		t.Fatalf("conformance: render normalised step: %v", err)
	}

	return string(pretty)
}

func decode(t *testing.T, value any) any {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("conformance: re-encode transcript: %v", err)
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("conformance: decode transcript: %v", err)
	}

	return decoded
}
