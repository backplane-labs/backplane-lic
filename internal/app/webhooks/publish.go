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

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// Delivery is one claimed outbox row with its endpoint's live URL and
// signature algorithm (the delivery worker's unit of work).
type Delivery struct {
	Event domain.WebhookEvent
	// EndpointURL is the endpoint's current URL (rows whose endpoint
	// row is gone are never claimed).
	EndpointURL        string
	SignatureAlgorithm string
}

// PublisherStore is the publisher's storage port: the subscribed
// endpoints, the outbox writes and the lookups the payload and the
// product filter need.
type PublisherStore interface {
	SubscribedWebhookEndpoints(
		ctx context.Context, accountID uuid.UUID, event string,
	) ([]domain.WebhookEndpoint, error)
	InsertWebhookEvent(ctx context.Context, event domain.WebhookEvent) error

	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	UserLicensedToProduct(ctx context.Context, accountID, userID, productID uuid.UUID) (bool, error)
	PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error)
}

// Publisher is the real events.Publisher: inside the caller's
// transaction it writes one webhook_events row per subscribed endpoint
// (the outbox), with the payload the delivery POSTs carry.
type Publisher struct {
	store     PublisherStore
	accountID uuid.UUID
	rng       io.Reader
	clock     func() time.Time
}

// NewPublisher builds the publisher over the store (the account's id:
// the installation's single account).
func NewPublisher(
	store PublisherStore, accountID uuid.UUID, rng io.Reader, clock func() time.Time,
) *Publisher {
	return &Publisher{store: store, accountID: accountID, rng: rng, clock: clock}
}

// Publish records the event for delivery to every endpoint subscribed
// to it. The queries ride the caller's context, so inside a store.Do
// the rows land in the caller's transaction — the change and its
// events commit together or not at all.
func (p *Publisher) Publish(ctx context.Context, event string, resource any) error {
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
			NextAttemptAt:    &now,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		if err := p.store.InsertWebhookEvent(ctx, row); err != nil {
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
	case domain.License:
		document = oas.LicenseDocument{
			Data: oas.NilLicenseResource{Value: render.License(p.accountID, v, p.clock(), nil)},
		}
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
	case oas.LicenseDocument:
		doc.Encode(encoder)
	case oas.LicenseUserListDocument:
		doc.Encode(encoder)
	case oas.LicenseEntitlementListDocument:
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
