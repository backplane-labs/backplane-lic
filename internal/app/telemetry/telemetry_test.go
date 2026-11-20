package telemetry_test

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/telemetry"
)

func TestOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "ok", want: "ok"},
		{name: "refusal", err: apperr.Forbidden("LICENSE_SUSPENDED", "suspended"), want: "LICENSE_SUSPENDED"},
		{name: "codeless refusal", err: apperr.ForbiddenReason(""), want: "refused"},
		{name: "technical", err: errors.New("db down"), want: "error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := telemetry.Outcome(tc.err); got != tc.want {
				t.Fatalf("%q, want %q", got, tc.want)
			}
		})
	}
}

// The counters record with bounded labels; the zero value records nothing.
func TestCounters(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	c := telemetry.New(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("lic"))
	ctx := context.Background()

	c.Validation(ctx, "EXPIRED")
	c.Activation(ctx, nil)
	c.Checkout(ctx, "machine", apperr.Forbidden("LICENSE_BANNED", "banned"))
	c.UsageReport(ctx, nil)
	telemetry.Counters{}.Validation(ctx, "VALID")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}

	got := map[string]int64{}

	for _, m := range rm.ScopeMetrics[0].Metrics {
		sum, _ := m.Data.(metricdata.Sum[int64])
		for _, p := range sum.DataPoints {
			got[m.Name+"{"+p.Attributes.Encoded(attribute.DefaultEncoder())+"}"] = p.Value
		}
	}

	for _, want := range []string{
		"lic.license.validations{code=EXPIRED}", "lic.machine.activations{outcome=ok}",
		"lic.checkouts{kind=machine,outcome=LICENSE_BANNED}", "lic.usage.reports{outcome=ok}",
	} {
		if got[want] != 1 {
			t.Errorf("%s = %d (%v)", want, got[want], got)
		}
	}
}
