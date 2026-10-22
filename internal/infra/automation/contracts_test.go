package automation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"
	"github.com/backplane-labs/backplane/pkg/backplane/hook"

	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/infra/automation"
)

func TestDeliveryBindingDisappeared(t *testing.T) {
	t.Parallel()
	harness := backplanetest.New(t)
	contracts := automation.Declare(harness.Root())
	backplanetest.Answer(harness, contracts.DeliverHook, func(context.Context, port.DeliverIn) (port.DeliverOut, error) {
		return port.DeliverOut{}, hook.ErrNoBinding
	})

	if !contracts.Bound() {
		t.Fatal("test hook should be locally bound")
	}

	if err := contracts.Deliver(t.Context(), port.DeliverIn{}, "stable-key"); err != nil {
		t.Fatalf("removed binding must preserve webhook-only fallback: %v", err)
	}
}

func TestOtherHookErrorsRemainRetryable(t *testing.T) {
	t.Parallel()
	harness := backplanetest.New(t)
	contracts := automation.Declare(harness.Root())
	failure := errors.New("courier unavailable")

	backplanetest.Answer(harness, contracts.DeliverHook, func(context.Context, port.DeliverIn) (port.DeliverOut, error) {
		return port.DeliverOut{}, failure
	})

	if err := contracts.Deliver(t.Context(), port.DeliverIn{}, "stable-key"); !errors.Is(err, failure) {
		t.Fatalf("delivery must preserve failure for the outbox: %v", err)
	}
}

func TestAutomationManifest(t *testing.T) {
	t.Parallel()
	harness := backplanetest.New(t, backplanetest.Name("lic"))
	contracts := automation.Declare(harness.Root())
	automation.NewActivityServer(harness.Root())

	if contracts.DeliverHook.Name() != "lic.Deliver" {
		t.Fatalf("hook = %s", contracts.DeliverHook.Name())
	}

	manifest := harness.Manifest()
	if len(manifest.GetHooks()) != 1 || len(manifest.GetEvents()) != 10 || len(manifest.GetActivities()) != 6 {
		t.Fatalf("manifest hooks/events/activities = %d/%d/%d", len(manifest.GetHooks()),
			len(manifest.GetEvents()), len(manifest.GetActivities()))
	}
}
