// Package automation declares the service-owned Backplane contracts and
// dispatches committed outbox records to the SDK.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/event"
	"github.com/backplane-labs/backplane/pkg/backplane/hook"

	outbox "github.com/gopherex/pg-outbox"

	app "github.com/backplane-labs/backplane-lic/internal/app/automation"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
)

// Contracts is immutable after construction; SDK refs own their runtime state.
type Contracts struct {
	DeliverHook            hook.Ref[port.DeliverIn, port.DeliverOut]
	UserCreated            event.Ref[port.UserEvent]
	PasswordResetRequested event.Ref[port.UserEvent]
	LicenseEvents          map[string]event.Ref[port.LicenseEvent]
}

func Declare(scope deps.Scope) Contracts {
	refs := Contracts{
		DeliverHook: hook.Declare[port.DeliverIn, port.DeliverOut](scope, "Deliver",
			hook.DefaultTimeout(time.Minute), hook.Describe("Render and deliver an installation-owned licensing message")),
		UserCreated:            event.Declare[port.UserEvent](scope, "UserCreated"),
		PasswordResetRequested: event.Declare[port.UserEvent](scope, "PasswordResetRequested"),
		LicenseEvents:          make(map[string]event.Ref[port.LicenseEvent]),
	}
	for _, name := range []string{
		"LicenseCreated", "LicenseExpiringSoon", "LicenseExpired", "LicenseRenewed",
		"LicenseSuspended", "LicenseRevoked", "LicenseValidationFailed", "UsageIncremented",
	} {
		refs.LicenseEvents[name] = event.Declare[port.LicenseEvent](scope, name)
	}

	return refs
}

func (c Contracts) Bound() bool { return c.DeliverHook.Bound() }

func (c Contracts) Deliver(ctx context.Context, in port.DeliverIn, key string) error {
	if !c.Bound() {
		return nil
	}

	_, err := c.DeliverHook.Call(ctx, in, hook.Key(key))
	if errors.Is(err, hook.ErrNoBinding) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("lic.Deliver: %w", err)
	}

	return nil
}

// Worker multiplexes the shared outbox. A hook execution uses the persisted
// message ID as its key, so a lost reply does not create a fresh workflow.
type Worker struct {
	contracts Contracts
	webhooks  outbox.Publisher
}

func NewWorker(contracts Contracts, webhooks outbox.Publisher) Worker {
	return Worker{contracts: contracts, webhooks: webhooks}
}

func (w Worker) Publish(ctx context.Context, messages []outbox.Message) error {
	for i := range messages {
		message := messages[i]
		var err error

		switch message.Topic {
		case app.DeliveryTopic:
			var in port.DeliverIn
			if err = json.Unmarshal(message.Payload, &in); err == nil {
				err = w.contracts.Deliver(ctx, in, message.ID)
			} else {
				err = outbox.Permanent(err)
			}
		case app.EventTopic:
			err = w.publishEvent(ctx, message)
		default:
			err = w.webhooks.Publish(ctx, []outbox.Message{message})
		}

		if err != nil {
			return fmt.Errorf("automation: relay %s: %w", message.ID, err)
		}
	}

	return nil
}

var errUnknownEvent = errors.New("unknown automation event")

//nolint:wrapcheck // Publish wraps this error with the persisted message identity.
func (w Worker) publishEvent(ctx context.Context, message outbox.Message) error {
	opts := []event.PublishOption{event.ID(message.ID)}
	switch message.MessageType {
	case "UserCreated", "PasswordResetRequested":
		var in port.UserEvent
		if err := json.Unmarshal(message.Payload, &in); err != nil {
			return outbox.Permanent(err)
		}

		opts = append(opts, event.Key(in.UserID.String()))

		ref := w.contracts.UserCreated
		if message.MessageType == "PasswordResetRequested" {
			ref = w.contracts.PasswordResetRequested
		}

		return ref.Publish(ctx, in, opts...)
	default:
		ref, ok := w.contracts.LicenseEvents[message.MessageType]
		if !ok {
			return outbox.Permanent(fmt.Errorf("%w: %q", errUnknownEvent, message.MessageType))
		}

		var in port.LicenseEvent
		if err := json.Unmarshal(message.Payload, &in); err != nil {
			return outbox.Permanent(err)
		}

		return ref.Publish(ctx, in, append(opts, event.Key(in.LicenseID.String()))...)
	}
}
