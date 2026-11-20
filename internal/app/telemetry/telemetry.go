// Package telemetry is lic's business counters: validations by their code,
// activations, check-outs and usage reports by outcome. Labels are bounded:
// Keygen's validation codes, the refusal codes, ok, error.
package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
)

// Counters are the business counters; the zero value records nothing.
type Counters struct {
	validations metric.Int64Counter
	activations metric.Int64Counter
	checkouts   metric.Int64Counter
	reports     metric.Int64Counter
	webhooks    metric.Int64Counter
}

// New is the counters on meter.
func New(meter metric.Meter) Counters {
	// Instrument creation fails only on an invalid name, fixed here.
	validations, _ := meter.Int64Counter("lic.license.validations",
		metric.WithDescription("License validations by Keygen's validation code"))
	activations, _ := meter.Int64Counter("lic.machine.activations",
		metric.WithDescription("Machine activations by outcome (ok or the refusal code)"))
	checkouts, _ := meter.Int64Counter("lic.checkouts",
		metric.WithDescription("License and machine file check-outs by kind and outcome"))
	reports, _ := meter.Int64Counter("lic.usage.reports",
		metric.WithDescription("Signed usage reports by outcome (ok or the refusal code)"))

	webhooks, _ := meter.Int64Counter("lic.webhook.deliveries",
		metric.WithDescription("Webhook delivery attempts by outcome (delivered, failing: retried, failed: final)"))

	return Counters{
		validations: validations, activations: activations, checkouts: checkouts, reports: reports, webhooks: webhooks,
	}
}

// Validation counts a validation answered with code.
func (c Counters) Validation(ctx context.Context, code string) {
	add(ctx, c.validations, attribute.String("code", code))
}

// Activation counts an activation that ended with err.
func (c Counters) Activation(ctx context.Context, err error) {
	add(ctx, c.activations, attribute.String("outcome", Outcome(err)))
}

// Checkout counts a check-out of kind (license, machine) that ended with err.
func (c Counters) Checkout(ctx context.Context, kind string, err error) {
	add(ctx, c.checkouts, attribute.String("kind", kind), attribute.String("outcome", Outcome(err)))
}

// UsageReport counts a usage report that ended with err.
func (c Counters) UsageReport(ctx context.Context, err error) {
	add(ctx, c.reports, attribute.String("outcome", Outcome(err)))
}

// Webhook counts a webhook delivery attempt ending in outcome (delivered,
// failing, failed).
func (c Counters) Webhook(ctx context.Context, outcome string) {
	add(ctx, c.webhooks, attribute.String("outcome", outcome))
}

func add(ctx context.Context, counter metric.Int64Counter, attrs ...attribute.KeyValue) {
	if counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// Outcome is ok, a refusal's code (or its HTTP class when it has none), or
// error for a technical failure.
func Outcome(err error) string {
	if err == nil {
		return "ok"
	}

	var refusal *apperr.T
	if !errors.As(err, &refusal) {
		return "error"
	}

	if refusal.Code != "" {
		return refusal.Code
	}

	return "refused"
}
