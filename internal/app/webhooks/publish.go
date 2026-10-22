package webhooks

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// PublisherStore is the publisher's storage port: the subscribed
// endpoints, the outbox writes and the lookups the payload and the
// product filter need.
type PublisherStore = port.WebhookPublisherStore

// Publisher is the real events.Publisher: inside the caller's
// transaction it writes one webhook_events row per subscribed endpoint
// and enqueues a reference with the payload the delivery POSTs carry.
type Publisher struct {
	store     PublisherStore
	queue     port.WebhookQueue
	accountID uuid.UUID
	rng       io.Reader
	clock     func() time.Time
}

// NewPublisher builds the publisher over the store (the account's id:
// the installation's single account).
func NewPublisher(
	store PublisherStore, queue port.WebhookQueue, accountID uuid.UUID, rng io.Reader, clock func() time.Time,
) *Publisher {
	return &Publisher{store: store, queue: queue, accountID: accountID, rng: rng, clock: clock}
}

// Publish records the event for delivery to every endpoint subscribed
// to it. The queries ride the caller's context, so inside a store.Do
// the rows land in the caller's transaction — the change and its
// events commit together or not at all.
func (p *Publisher) Publish(ctx context.Context, event string, resource any) error {
	return p.store.Do(ctx, func(ctx context.Context) error { //nolint:wrapcheck // preserve the transaction error
		return p.publish(ctx, event, resource)
	})
}

func (p *Publisher) publish(ctx context.Context, event string, resource any) error {
	endpoints, err := p.store.SubscribedWebhookEndpoints(ctx, p.accountID, event)
	if err != nil {
		return fmt.Errorf("webhooks: publish %s: endpoints: %w", event, err)
	}

	if len(endpoints) == 0 {
		return nil
	}

	payload, err := p.payload(ctx, resource)
	if err != nil {
		return fmt.Errorf("webhooks: publish %s: %w", event, err)
	}

	now := p.clock()

	for i := range endpoints {
		endpoint := endpoints[i]

		allowed, err := p.productMayRead(ctx, endpoint, resource)
		if err != nil {
			return fmt.Errorf("webhooks: publish %s: product filter: %w", event, err)
		}

		if !allowed {
			continue
		}

		token, err := idempotencyToken(p.rng)
		if err != nil {
			return fmt.Errorf("webhooks: publish %s: token: %w", event, err)
		}

		row := domain.WebhookEvent{
			ID:               uuid.New(),
			AccountID:        p.accountID,
			EndpointID:       uuid.NullUUID{UUID: endpoint.ID, Valid: true},
			Endpoint:         endpoint.URL,
			Event:            event,
			Payload:          payload,
			APIVersion:       endpoint.APIVersion,
			IdempotencyToken: token,
			Status:           domain.EventDelivering,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		if err := record(ctx, p.store, p.queue, row); err != nil {
			return fmt.Errorf("webhooks: publish %s: insert: %w", event, err)
		}
	}

	return nil
}

// payload renders the event's resource document (the same renderer the
// API responses use) as the compact JSON string the wire carries.
func (p *Publisher) payload(ctx context.Context, resource any) (string, error) {
	var document any

	switch v := resource.(type) {
	case domain.User:
		active, err := p.store.UserHasActiveLicense(ctx, p.accountID, v.ID)
		if err != nil {
			return "", fmt.Errorf("user status: %w", err)
		}

		document = oas.UserDocument{Data: render.User(p.accountID, v, v.Status(active, p.clock()))}
	default:
		doc, err := render.PayloadDocument(p.accountID, resource)
		if err != nil {
			return "", err //nolint:wrapcheck // the renderer's own error
		}

		document = doc
	}

	encoder := jx.Encoder{}
	if err := encode(document, &encoder); err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	return encoder.String(), nil
}

// productMayRead answers the product-scoped endpoint's read check for
// the resource (the lookup-needing cases resolve here).
func (p *Publisher) productMayRead(
	ctx context.Context, endpoint domain.WebhookEndpoint, resource any,
) (bool, error) {
	if !endpoint.ProductID.Valid {
		return true, nil
	}

	product := endpoint.ProductID.UUID

	switch v := resource.(type) {
	case domain.Token:
		if v.BearerType == "User" {
			licensed, err := p.store.UserLicensedToProduct(ctx, p.accountID, v.BearerID, product)

			return licensed, fmt.Errorf("token bearer licensed: %w", err)
		}
	case domain.PolicyEntitlement:
		policy, err := p.store.PolicyByID(ctx, p.accountID, v.PolicyID)
		if err != nil {
			return false, fmt.Errorf("policy of join: %w", err)
		}

		return policy.ProductID == product, nil
	case domain.User:
		// UserPolicy#show? admits any product for user-role records.
		return v.Role == domain.RoleUser, nil
	}

	return access.ProductMayRead(product, resource), nil
}

// encode writes the document with its generated encoder (the stdlib
// MarshalJSON of the oas wrappers cannot express an unset optional).
func encode(document any, encoder *jx.Encoder) error {
	switch doc := document.(type) {
	case oas.UserDocument:
		doc.Encode(encoder)
	case oas.SecondFactorDocument:
		doc.Encode(encoder)
	case oas.ProductDocument:
		doc.Encode(encoder)
	case oas.PolicyDocument:
		doc.Encode(encoder)
	case oas.EntitlementDocument:
		doc.Encode(encoder)
	case oas.GroupDocument:
		doc.Encode(encoder)
	case oas.GroupOwnerCollectionDocument:
		doc.Encode(encoder)
	case oas.PolicyEntitlementListDocument:
		doc.Encode(encoder)
	case oas.KeyDocument:
		doc.Encode(encoder)
	case oas.TokenDocument:
		doc.Encode(encoder)
	default:
		return fmt.Errorf("unsupported document %T: %w", document, ErrNoResource)
	}

	return nil
}

// idempotencyBytes is the random body of the idempotency token (hex
// to 64 chars).
const idempotencyBytes = 32

// idempotencyToken generates the reference's Tokenable v3 form:
// 32 random bytes hex-encoded (64 chars), the last two replaced by the
// version marker.
func idempotencyToken(rng io.Reader) (string, error) {
	buf := make([]byte, idempotencyBytes)
	if _, err := io.ReadFull(rng, buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}

	token := []byte(hex.EncodeToString(buf))
	copy(token[len(token)-2:], "v3")

	return string(token), nil
}

// ErrNoResource answers a publish of a value the publisher cannot
// render (every event must carry its resource document).
var ErrNoResource = errors.New("webhooks: event without a renderable resource")

// record writes the API resource and its delivery reference in one transaction.
// Both callers own the pgtx boundary; Enqueue resolves that context's tx.DB.
func record(ctx context.Context, store port.WebhookEventWriter,
	queue port.WebhookQueue, event domain.WebhookEvent,
) error {
	if err := store.InsertWebhookEvent(ctx, event); err != nil {
		return fmt.Errorf("webhooks: insert event: %w", err)
	}

	if err := queue.Enqueue(ctx, outbox.Message{
		ID: event.ID.String(), Topic: "webhooks", PartitionKey: event.AccountID.String(),
		Payload: []byte(event.ID.String()), ContentType: "text/plain", MessageType: "webhook-event",
	}); err != nil {
		return fmt.Errorf("webhooks: enqueue event: %w", err)
	}

	return nil
}
