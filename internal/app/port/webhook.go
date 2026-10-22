package port

import (
	"context"

	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// WebhookQueue enqueues delivery references in the caller's transaction.
// The outbox message contract is shared with the delivery adapter.
type WebhookQueue interface {
	Enqueue(ctx context.Context, message outbox.Message) error
}

type WebhooksStore interface { //nolint:interfacebloat // one port per scenario is the architecture's shape
	// Do runs fn in one transaction; queries carrying the returned
	// context join it.
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	WebhookEndpointByID(ctx context.Context, accountID, id uuid.UUID) (domain.WebhookEndpoint, error)
	WebhookEndpointByURL(ctx context.Context, accountID uuid.UUID, url string) (domain.WebhookEndpoint, error)
	InsertWebhookEndpoint(ctx context.Context, endpoint domain.WebhookEndpoint) error
	UpdateWebhookEndpoint(ctx context.Context, endpoint domain.WebhookEndpoint) error
	DeleteWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error
	ListWebhookEndpoints(
		ctx context.Context, accountID uuid.UUID, product uuid.NullUUID, page paging.Params,
	) ([]domain.WebhookEndpoint, error)
	CountWebhookEndpoints(ctx context.Context, accountID uuid.UUID, product uuid.NullUUID) (int, error)

	InsertWebhookEvent(ctx context.Context, event domain.WebhookEvent) error
	WebhookEventByID(ctx context.Context, accountID, id uuid.UUID) (domain.WebhookEvent, error)
	ListWebhookEvents(
		ctx context.Context, accountID uuid.UUID, filter WebhookEventFilter, product uuid.NullUUID,
		page paging.Params,
	) ([]domain.WebhookEvent, error)
	CountWebhookEvents(
		ctx context.Context, accountID uuid.UUID, filter WebhookEventFilter, product uuid.NullUUID,
	) (int, error)
	DeleteWebhookEvent(ctx context.Context, accountID, id uuid.UUID) error

	// WebhookEndpointProduct answers the event's endpoint's product
	// (the product bearer's read scope); ErrNotFound when the endpoint
	// row is gone.
	WebhookEndpointProduct(ctx context.Context, accountID, endpointID uuid.UUID) (uuid.NullUUID, error)
}

// WebhookEventFilter is the webhook-events index filter.
type WebhookEventFilter struct {
	// Events filters by exact event names (events[]); an unknown name
	// simply matches nothing.
	Events []string
}

type WebhookPublisherStore interface {
	Do(ctx context.Context, fn func(context.Context) error) error
	SubscribedWebhookEndpoints(
		ctx context.Context, accountID uuid.UUID, event string,
	) ([]domain.WebhookEndpoint, error)
	InsertWebhookEvent(ctx context.Context, event domain.WebhookEvent) error

	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	UserLicensedToProduct(ctx context.Context, accountID, userID, productID uuid.UUID) (bool, error)
	PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error)
}

type WebhookDeliveryStore interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
	WebhookEventByID(ctx context.Context, accountID, id uuid.UUID) (domain.WebhookEvent, error)
	WebhookEndpointByID(ctx context.Context, accountID, id uuid.UUID) (domain.WebhookEndpoint, error)
	SetWebhookEventDelivered(ctx context.Context, accountID, id uuid.UUID, code int, body string) error
	SetWebhookEventFailure(
		ctx context.Context, accountID, id uuid.UUID, status domain.WebhookEventStatus,
		code *int, body string,
	) error
	SetWebhookEventExhausted(ctx context.Context, accountID, id uuid.UUID) error
	DisableWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error
}

// WebhookEventWriter persists the Keygen API resource.
type WebhookEventWriter interface {
	InsertWebhookEvent(ctx context.Context, event domain.WebhookEvent) error
}
