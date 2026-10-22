// Package webhooks is the webhook scenario: endpoint CRUD (the URL,
// subscription and signature rules), the webhook-events routes (index,
// show, destroy, retry) and the event publisher that writes the outbox
// rows inside the caller's transaction.
package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// Store is the scenario's storage port (satisfied by *postgres.Store;
// Do runs a transaction the publisher's writes join).
type Store = port.WebhooksStore

// EventFilter is the webhook-events index filter.
type EventFilter = port.WebhookEventFilter

// Input is the endpoint create/update body after the handler's
// typed-params validation (types, unpermitted keys, apiVersion
// inclusion — the reference rejects those as 400s before the model).
type Input struct {
	// URL is the raw url attribute; nil when the update leaves it.
	URL *string
	// Subscriptions is the deduped subscription list; nil when the
	// update leaves it (["*"] is the create default).
	Subscriptions   []string
	SubscriptionsOn bool
	// SignatureAlgorithm nil means unset (ed25519 on create).
	SignatureAlgorithm *string
	// APIVersion carries an explicit apiVersion attribute (already
	// validated by the handler); nil when absent.
	APIVersion *string
	// Product carries the product relationship: nil pointer = present
	// with null data (clears); ProductOn false = absent.
	Product   *uuid.UUID
	ProductOn bool
}

// Scenario is the webhook endpoints and events scenario.
type Scenario struct {
	store    Store
	resolver domain.Resolver
	allowed  []*net.IPNet
	queue    port.WebhookQueue
	clock    func() time.Time
}

// New builds the scenario (resolver, allowlist, queue and clock
// injectable for tests).
func New(
	store Store, resolver domain.Resolver, allowedPrivate []*net.IPNet,
	queue port.WebhookQueue, clock func() time.Time,
) Scenario {
	return Scenario{store: store, resolver: resolver, allowed: allowedPrivate, queue: queue, clock: clock}
}

// The docs anchors of the endpoint's validation errors.
const (
	aboutURL       = "webhook-endpoints/#webhook-endpoints-object-attrs-url"
	aboutSubs      = "webhook-endpoints/#webhook-endpoints-object-attrs-subscriptions"
	aboutSignature = "webhook-endpoints/#webhook-endpoints-object-attrs-signatureAlgorithm"
)

// Create builds the endpoint (the product inferred from a product
// bearer when the relationship is absent) and answers the 201 row.
func (s Scenario) Create(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	in Input,
) (domain.WebhookEndpoint, error) {
	if err := access.Require(ctx, "webhook-endpoint.create"); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	endpoint := domain.WebhookEndpoint{
		ID:                 uuid.New(),
		AccountID:          account.ID,
		URL:                *in.URL,
		Subscriptions:      []string{"*"},
		SignatureAlgorithm: domain.SignatureEd25519,
		APIVersion:         accounts.CurrentAPIVersion,
		CreatedAt:          s.clock(),
		UpdatedAt:          s.clock(),
	}

	applyInput(&endpoint, in)

	// The Zapier workaround: a product bearer scopes its endpoint to
	// itself even without the relationship.
	if principal.Role == domain.RoleProduct && !endpoint.ProductID.Valid {
		endpoint.ProductID = uuid.NullUUID{UUID: principal.BearerID, Valid: true}
	}

	if err := access.WebhookEndpointWrite(principal, endpoint); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.validate(endpoint); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := s.store.InsertWebhookEndpoint(ctx, endpoint); err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhooks: insert endpoint: %w", err)
	}

	return endpoint, nil
}

// Update applies the partial body and answers the row.
func (s Scenario) Update(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	id uuid.UUID,
	in Input,
) (domain.WebhookEndpoint, error) {
	endpoint, err := s.show(ctx, account.ID, principal, id)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := access.Require(ctx, "webhook-endpoint.update"); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	applyInput(&endpoint, in)

	if err := access.WebhookEndpointWrite(principal, endpoint); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.validate(endpoint); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	endpoint.UpdatedAt = s.clock()

	if err := s.store.UpdateWebhookEndpoint(ctx, endpoint); err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhooks: update endpoint: %w", err)
	}

	return endpoint, nil
}

// Show answers the endpoint.
func (s Scenario) Show(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.WebhookEndpoint, error) {
	endpoint, err := s.show(ctx, account.ID, principal, id)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := access.Require(ctx, "webhook-endpoint.read"); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	return endpoint, nil
}

func (s Scenario) show(
	ctx context.Context, accountID uuid.UUID, principal access.Principal, id uuid.UUID,
) (domain.WebhookEndpoint, error) {
	// Endpoint has no for_user/for_license/for_product scope in CE, so
	// these bearers' member lookups miss before permissions are checked.
	if principal.Role == domain.RoleProduct || principal.Role == domain.RoleUser || principal.Role == domain.RoleLicense {
		return domain.WebhookEndpoint{}, apperr.NotFound("webhook endpoint", id.String())
	}

	endpoint, err := s.store.WebhookEndpointByID(ctx, accountID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.WebhookEndpoint{}, apperr.NotFound("webhook endpoint", id.String())
	}

	if err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the store's own error
	}

	if err := access.WebhookEndpointShow(principal, endpoint); err != nil {
		return domain.WebhookEndpoint{}, err //nolint:wrapcheck // the authorization's own error
	}

	return endpoint, nil
}

// Index answers the (product-scoped) page and its total.
func (s Scenario) Index(
	ctx context.Context, account domain.Account, principal access.Principal, page paging.Params,
) ([]domain.WebhookEndpoint, int, error) {
	if err := access.Require(ctx, "webhook-endpoint.read"); err != nil {
		return nil, 0, err //nolint:wrapcheck // the authorization's own error
	}

	// The product bearer's authorized scope is empty (the reference's
	// scoped query returns nothing for a product on endpoints).
	if principal.Role == domain.RoleProduct {
		return []domain.WebhookEndpoint{}, 0, nil
	}

	items, err := s.store.ListWebhookEndpoints(ctx, account.ID, uuid.NullUUID{}, page)
	if err != nil {
		return nil, 0, err //nolint:wrapcheck // the store's own error
	}

	if err := access.WebhookEndpointIndex(principal, items); err != nil {
		return nil, 0, err //nolint:wrapcheck // the authorization's own error
	}

	count, err := s.store.CountWebhookEndpoints(ctx, account.ID, uuid.NullUUID{})
	if err != nil {
		return nil, 0, err //nolint:wrapcheck // the store's own error
	}

	return items, count, nil
}

// Destroy removes the endpoint.
func (s Scenario) Destroy(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) error {
	endpoint, err := s.show(ctx, account.ID, principal, id)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "webhook-endpoint.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.WebhookEndpointWrite(principal, endpoint); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.store.DeleteWebhookEndpoint(ctx, account.ID, endpoint.ID); err != nil {
		return fmt.Errorf("webhooks: delete endpoint: %w", err)
	}

	return nil
}

// EventsIndex answers the filtered (product-scoped) page and total.
func (s Scenario) EventsIndex(
	ctx context.Context, account domain.Account, principal access.Principal,
	filter EventFilter, page paging.Params,
) ([]domain.WebhookEvent, int, error) {
	if err := access.Require(ctx, "webhook-event.read"); err != nil {
		return nil, 0, err //nolint:wrapcheck // the authorization's own error
	}

	scope := productScope(principal)

	items, err := s.store.ListWebhookEvents(ctx, account.ID, filter, scope, page)
	if err != nil {
		return nil, 0, err //nolint:wrapcheck // the store's own error
	}

	productOf := func(endpointID uuid.UUID) uuid.UUID {
		id, err := s.store.WebhookEndpointProduct(ctx, account.ID, endpointID)
		if err != nil {
			return uuid.Nil
		}

		return id.UUID
	}

	if err := access.WebhookEventIndex(principal, items, productOf); err != nil {
		return nil, 0, err //nolint:wrapcheck // the authorization's own error
	}

	count, err := s.store.CountWebhookEvents(ctx, account.ID, filter, scope)
	if err != nil {
		return nil, 0, err //nolint:wrapcheck // the store's own error
	}

	return items, count, nil
}

// EventShow answers the event.
func (s Scenario) EventShow(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.WebhookEvent, error) {
	event, err := s.loadEventScoped(ctx, account.ID, principal, id)
	if err != nil {
		return domain.WebhookEvent{}, err
	}

	if err := access.Require(ctx, "webhook-event.read"); err != nil {
		return domain.WebhookEvent{}, err //nolint:wrapcheck // the authorization's own error
	}

	product, err := s.eventProduct(ctx, account.ID, event)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.WebhookEvent{}, err
	}

	if err := access.WebhookEventShow(principal, event, product.UUID, product.Valid); err != nil {
		return domain.WebhookEvent{}, err //nolint:wrapcheck // the authorization's own error
	}

	return event, nil
}

// EventDestroy removes the event (no delivery side effects).
func (s Scenario) EventDestroy(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) error {
	event, err := s.loadEventScoped(ctx, account.ID, principal, id)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "webhook-event.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.WebhookEventManage(principal); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.store.DeleteWebhookEvent(ctx, account.ID, event.ID); err != nil {
		return fmt.Errorf("webhooks: delete event: %w", err)
	}

	return nil
}

// Retry enqueues a fresh delivery of the event (a NEW row copying the
// payload, api version and idempotency token; the original stays as
// is). The reference's generic unprocessable entity answers an event
// whose endpoint is gone.
func (s Scenario) Retry(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.WebhookEvent, error) {
	original, err := s.loadEventScoped(ctx, account.ID, principal, id)
	if err != nil {
		return domain.WebhookEvent{}, err
	}

	if err := access.Require(ctx, "webhook-event.retry"); err != nil {
		return domain.WebhookEvent{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.WebhookEventManage(principal); err != nil {
		return domain.WebhookEvent{}, err //nolint:wrapcheck // the authorization's own error
	}

	endpoint, err := s.retryEndpoint(ctx, account.ID, original)
	if errors.Is(err, ErrNotFound) {
		return domain.WebhookEvent{}, retryFailed()
	}

	if err != nil {
		return domain.WebhookEvent{}, err
	}

	now := s.clock()

	retry := domain.WebhookEvent{
		ID:               uuid.New(),
		AccountID:        account.ID,
		EndpointID:       uuid.NullUUID{UUID: endpoint.ID, Valid: true},
		Endpoint:         original.Endpoint,
		Event:            original.Event,
		Payload:          original.Payload,
		APIVersion:       original.APIVersion,
		IdempotencyToken: original.IdempotencyToken,
		Status:           domain.EventDelivering,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		return record(ctx, s.store, s.queue, retry)
	}); err != nil {
		return domain.WebhookEvent{}, fmt.Errorf("webhooks: insert retry: %w", err)
	}

	return retry, nil
}

// retryEndpoint resolves the endpoint the retry delivers to: the
// event's own, else the first account endpoint with the snapshot URL.
func (s Scenario) retryEndpoint(
	ctx context.Context, accountID uuid.UUID, event domain.WebhookEvent,
) (domain.WebhookEndpoint, error) {
	if event.EndpointID.Valid {
		endpoint, err := s.store.WebhookEndpointByID(ctx, accountID, event.EndpointID.UUID)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return endpoint, err //nolint:wrapcheck // the store's own error
		}
	}

	return s.store.WebhookEndpointByURL(ctx, accountID, event.Endpoint) //nolint:wrapcheck // the store's own error
}

func (s Scenario) loadEventScoped(
	ctx context.Context, accountID uuid.UUID, principal access.Principal, id uuid.UUID,
) (domain.WebhookEvent, error) {
	// Webhook events have no user/license scope; member lookups miss
	// before the action-specific permission check runs.
	if principal.Role == domain.RoleUser || principal.Role == domain.RoleLicense {
		return domain.WebhookEvent{}, apperr.NotFound("webhook event", id.String())
	}

	return s.load(ctx, accountID, id)
}

func (s Scenario) load(ctx context.Context, accountID, id uuid.UUID) (domain.WebhookEvent, error) {
	event, err := s.store.WebhookEventByID(ctx, accountID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.WebhookEvent{}, apperr.NotFound("webhook event", id.String())
	}

	return event, err //nolint:wrapcheck // the store's own error
}

func (s Scenario) eventProduct(
	ctx context.Context, accountID uuid.UUID, event domain.WebhookEvent,
) (uuid.NullUUID, error) {
	if !event.EndpointID.Valid {
		return uuid.NullUUID{}, nil
	}

	return s.store.WebhookEndpointProduct( //nolint:wrapcheck // the store's own error
		ctx, accountID, event.EndpointID.UUID)
}

// validate runs the model-level rules (the reference's 422s): the URL
// validator (protocol, host, address — errors accumulate), the
// subscription list and the signature algorithm.
func (s Scenario) validate(endpoint domain.WebhookEndpoint) error {
	var errs []*apperr.T

	if len(endpoint.Subscriptions) == 0 {
		errs = append(errs, apperr.Validation("SUBSCRIPTIONS_TOO_SHORT",
			"must have at least 1 webhook event subscription",
			"/data/attributes/subscriptions", aboutSubs))
	} else {
		wildcardOnly := len(endpoint.Subscriptions) == 1 && endpoint.Subscriptions[0] == "*"
		if !wildcardOnly {
			for _, sub := range endpoint.Subscriptions {
				if !domain.IsWebhookEvent(sub) {
					errs = append(errs, apperr.Validation("SUBSCRIPTIONS_NOT_ALLOWED",
						"unsupported webhook event type for subscription",
						"/data/attributes/subscriptions", aboutSubs))

					break
				}
			}
		}
	}

	for _, violation := range domain.ValidateWebhookURL(endpoint.URL, s.resolver, s.allowed) {
		errs = append(errs, apperr.Validation(violation.Code, violation.Detail,
			"/data/attributes/url", aboutURL))
	}

	if !slices.Contains(domain.SignatureAlgorithms, endpoint.SignatureAlgorithm) {
		errs = append(errs, apperr.Validation("SIGNATURE_ALGORITHM_NOT_ALLOWED",
			"unsupported signature algorithm",
			"/data/attributes/signatureAlgorithm", aboutSignature))
	}

	if len(errs) == 0 {
		return nil
	}

	return apperr.MultiOf(errs...) //nolint:wrapcheck // the validations' own errors
}

// applyInput layers the body over the endpoint (subscriptions deduped
// in first-appearance order, like the reference's uniq).
func applyInput(endpoint *domain.WebhookEndpoint, in Input) {
	if in.URL != nil {
		endpoint.URL = *in.URL
	}

	if in.SubscriptionsOn {
		endpoint.Subscriptions = dedup(in.Subscriptions)
	}

	if in.SignatureAlgorithm != nil {
		endpoint.SignatureAlgorithm = *in.SignatureAlgorithm
	}

	if in.APIVersion != nil {
		endpoint.APIVersion = *in.APIVersion
	}

	if in.ProductOn {
		if in.Product == nil {
			endpoint.ProductID = uuid.NullUUID{}
		} else {
			endpoint.ProductID = uuid.NullUUID{UUID: *in.Product, Valid: true}
		}
	}
}

func dedup(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out
}

// productScope answers the product bearer's index scope.
func productScope(principal access.Principal) uuid.NullUUID {
	if principal.Role == domain.RoleProduct {
		return uuid.NullUUID{UUID: principal.BearerID, Valid: true}
	}

	return uuid.NullUUID{}
}

// retryFailed is the reference's generic retry 422.
func retryFailed() error {
	return &apperr.T{
		Status: 422, //nolint:mnd // the reference's literal status
		Title:  apperr.TitleUnprocessable,
		Detail: "webhook event failed to retry",
	}
}
