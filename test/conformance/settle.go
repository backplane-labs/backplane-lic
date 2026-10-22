package conformance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type licenseTouchDocument struct {
	Data *struct {
		ID         string `json:"id"`
		Attributes struct {
			LastValidated *string `json:"lastValidated"` //nolint:tagliatelle // Keygen's wire spelling
		} `json:"attributes"`
	} `json:"data"`
}

// settleValidationTouch waits for the reference's TouchLicenseWorker. The
// response carries its in-memory timestamp immediately, but the next read
// can race the asynchronous database update. Synchronize that side effect
// without masking either response or weakening timestamp-presence checks.
func settleValidationTouch(tb testing.TB, target Target, step Step, result StepTranscript) {
	tb.Helper()

	if result.Status != http.StatusOK || !strings.Contains(step.Path, "/actions/validate") {
		return
	}

	raw, err := json.Marshal(result.Body)
	if err != nil {
		tb.Fatal(err)
	}

	var expected licenseTouchDocument
	if err := json.Unmarshal(raw, &expected); err != nil {
		tb.Fatal(err)
	}

	if expected.Data == nil || expected.Data.Attributes.LastValidated == nil {
		return
	}

	ctx, cancel := context.WithTimeout(tb.Context(), pollTimeout)
	defer cancel()

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		status, body := call(tb, target, http.MethodGet,
			"/v1/accounts/"+AccountSlug+"/licenses/"+expected.Data.ID, "", map[string]string{
				"Authorization": "Bearer " + AdminToken(tb, target),
			})

		var current licenseTouchDocument
		if status == http.StatusOK && json.Unmarshal(body, &current) == nil && current.Data != nil &&
			current.Data.Attributes.LastValidated != nil &&
			*current.Data.Attributes.LastValidated == *expected.Data.Attributes.LastValidated {
			return
		}

		select {
		case <-ctx.Done():
			tb.Fatalf("conformance: validation touch did not persist for license %s", expected.Data.ID)
		case <-ticker.C:
		}
	}
}
