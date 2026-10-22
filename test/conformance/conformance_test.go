package conformance_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

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

		for _, sc := range scns {
			t.Run(sc.Name, func(t *testing.T) {
				t.Parallel()

				transcript := conformance.Run(t, reference, sc, key)
				golden := filepath.Join("testdata", sc.Name+".golden.json")

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

	targetURL := os.Getenv("LIC_CONFORMANCE_TARGET")
	if targetURL == "" {
		t.Skip("LIC_CONFORMANCE_TARGET is not set — start backplane-lic and export its URL")
	}

	target := conformance.NewTarget(targetURL)
	key := conformance.Ed25519Key(t, target)

	for _, sc := range scns {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()

			golden := loadGolden(t, sc.Name)
			live := conformance.Run(t, target, sc, key)
			compare(t, golden, live)
		})
	}
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
