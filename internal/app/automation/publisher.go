// Package automation records installation automation in the same transaction
// as Keygen webhooks. The relay, never the request, calls external services.
package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"

	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

const (
	EventTopic    = "lic.events"
	DeliveryTopic = "lic.deliver"
)

// Publisher composes webhook and automation writes atomically, including when
// a scenario has not established its own transaction.
type Publisher struct {
	store      port.AutomationStore
	webhooks   events.Publisher
	queue      port.WebhookQueue
	delivery   port.Delivery
	portalBase string
	locale     string
	clock      func() time.Time
}

func NewPublisher(store port.AutomationStore, webhooks events.Publisher, queue port.WebhookQueue,
	delivery port.Delivery, portalBase, locale string, clock func() time.Time,
) Publisher {
	return Publisher{
		store: store, webhooks: webhooks, queue: queue, delivery: delivery,
		portalBase: portalBase, locale: locale, clock: clock,
	}
}

func (p Publisher) Publish(ctx context.Context, name string, resource any) error {
	if err := p.store.Do(ctx, func(ctx context.Context) error {
		if err := p.webhooks.Publish(ctx, name, resource); err != nil {
			return fmt.Errorf("webhook: %w", err)
		}

		return p.record(ctx, name, resource)
	}); err != nil {
		return fmt.Errorf("automation: publish %s: %w", name, err)
	}

	return nil
}

func (p Publisher) record(ctx context.Context, name string, resource any) error {
	switch value := resource.(type) {
	case domain.License:
		return p.recordLicense(ctx, name, value, "")
	case domain.LicenseValidationEvent:
		return p.recordLicense(ctx, name, value.License, value.Code)
	case domain.User:
		if name == "user.created" {
			return p.enqueue(ctx, EventTopic, "UserCreated", userEvent(value, p.clock()))
		}
	case domain.PasswordReset:
		if name != "user.password-reset" {
			return nil
		}

		if err := p.enqueue(ctx, EventTopic, "PasswordResetRequested", userEvent(value.User, p.clock())); err != nil {
			return err
		}

		if value.Deliver != nil && !*value.Deliver || !p.delivery.Bound() {
			return nil
		}

		return p.enqueue(ctx, DeliveryTopic, "Deliver", p.passwordReset(value))
	}

	return nil
}

func userEvent(user domain.User, now time.Time) port.UserEvent {
	return port.UserEvent{
		AccountID: user.AccountID, UserID: user.ID, Email: user.Email,
		Name: strings.TrimSpace(user.FirstName + " " + user.LastName), OccurredAt: now,
	}
}

func (p Publisher) passwordReset(reset domain.PasswordReset) port.DeliverIn {
	// Fragments stay in the browser, out of request logs and Referer headers.
	// The SPA submits token as passwordResetToken to the reset-password action.
	fragment := url.Values{
		"account": {reset.User.AccountID.String()}, "user": {reset.User.ID.String()}, "token": {reset.Token},
	}

	return port.DeliverIn{
		AccountID: reset.User.AccountID,
		Template:  "lic.password-reset", Recipient: reset.User.Email, Locale: p.locale,
		Data: port.DeliveryData{
			UserID: reset.User.ID, UserName: strings.TrimSpace(reset.User.FirstName + " " + reset.User.LastName),
			UserEmail: reset.User.Email, ResetURL: strings.TrimRight(p.portalBase, "/") + "/reset#" + fragment.Encode(),
		},
	}
}

func (p Publisher) enqueue(ctx context.Context, topic, name string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("automation: encode %s: %w", name, err)
	}

	var accountID uuid.UUID

	switch value := payload.(type) {
	case port.UserEvent:
		accountID = value.AccountID
	case port.LicenseEvent:
		accountID = value.AccountID
	case port.DeliverIn:
		accountID = value.AccountID
	}

	if err := p.queue.Enqueue(ctx, outbox.Message{
		ID: uuid.NewString(), Topic: topic, PartitionKey: accountID.String(),
		MessageType: name, ContentType: "application/json", Payload: encoded,
	}); err != nil {
		return fmt.Errorf("automation: enqueue %s: %w", name, err)
	}

	return nil
}
