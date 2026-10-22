package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/jobs"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The Store delegates the webhooks scenario's, the publisher's, the
// delivery worker's and the jobs runner's storage ports to its
// repositories, so the tree's single dependency satisfies them all.

// --- webhooks scenario ---

// WebhookEndpointByID loads an endpoint; ErrNotFound.
func (s *Store) WebhookEndpointByID(
	ctx context.Context, accountID, id uuid.UUID,
) (domain.WebhookEndpoint, error) {
	return s.Webhooks.WebhookEndpointByID(ctx, accountID, id)
}

// WebhookEndpointByURL loads the first endpoint with the URL.
func (s *Store) WebhookEndpointByURL(
	ctx context.Context, accountID uuid.UUID, url string,
) (domain.WebhookEndpoint, error) {
	return s.Webhooks.WebhookEndpointByURL(ctx, accountID, url)
}

// WebhookEndpointProduct answers an endpoint's product.
func (s *Store) WebhookEndpointProduct(
	ctx context.Context, accountID, id uuid.UUID,
) (uuid.NullUUID, error) {
	return s.Webhooks.WebhookEndpointProduct(ctx, accountID, id)
}

// InsertWebhookEndpoint writes a new endpoint row.
func (s *Store) InsertWebhookEndpoint(ctx context.Context, endpoint domain.WebhookEndpoint) error {
	return s.Webhooks.InsertWebhookEndpoint(ctx, endpoint)
}

// UpdateWebhookEndpoint writes every mutable column.
func (s *Store) UpdateWebhookEndpoint(ctx context.Context, endpoint domain.WebhookEndpoint) error {
	return s.Webhooks.UpdateWebhookEndpoint(ctx, endpoint)
}

// DeleteWebhookEndpoint removes the endpoint.
func (s *Store) DeleteWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Webhooks.DeleteWebhookEndpoint(ctx, accountID, id)
}

// DisableWebhookEndpoint empties the subscriptions.
func (s *Store) DisableWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Webhooks.DisableWebhookEndpoint(ctx, accountID, id)
}

// ListWebhookEndpoints answers the scoped page.
func (s *Store) ListWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, product uuid.NullUUID, page paging.Params,
) ([]domain.WebhookEndpoint, error) {
	return s.Webhooks.ListWebhookEndpoints(ctx, accountID, product, page)
}

// CountWebhookEndpoints answers the scoped total.
func (s *Store) CountWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, product uuid.NullUUID,
) (int, error) {
	return s.Webhooks.CountWebhookEndpoints(ctx, accountID, product)
}

// InsertWebhookEvent writes an outbox row.
func (s *Store) InsertWebhookEvent(ctx context.Context, event domain.WebhookEvent) error {
	return s.Webhooks.InsertWebhookEvent(ctx, event)
}

// WebhookEventByID loads an event; ErrNotFound.
func (s *Store) WebhookEventByID(
	ctx context.Context, accountID, id uuid.UUID,
) (domain.WebhookEvent, error) {
	return s.Webhooks.WebhookEventByID(ctx, accountID, id)
}

// ListWebhookEvents answers the filtered page.
func (s *Store) ListWebhookEvents(
	ctx context.Context, accountID uuid.UUID, filter webhooks.EventFilter,
	product uuid.NullUUID, page paging.Params,
) ([]domain.WebhookEvent, error) {
	return s.Webhooks.ListWebhookEvents(ctx, accountID, filter, product, page)
}

// CountWebhookEvents answers the filtered total.
func (s *Store) CountWebhookEvents(
	ctx context.Context, accountID uuid.UUID, filter webhooks.EventFilter, product uuid.NullUUID,
) (int, error) {
	return s.Webhooks.CountWebhookEvents(ctx, accountID, filter, product)
}

// DeleteWebhookEvent removes the event.
func (s *Store) DeleteWebhookEvent(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Webhooks.DeleteWebhookEvent(ctx, accountID, id)
}

// SubscribedWebhookEndpoints answers an event's fan-out candidates.
func (s *Store) SubscribedWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, event string,
) ([]domain.WebhookEndpoint, error) {
	return s.Webhooks.SubscribedWebhookEndpoints(ctx, accountID, event)
}

// --- delivery worker ---

// ClaimDueWebhookEvents claims the due batch (run inside the delivery
// transaction).
func (s *Store) ClaimDueWebhookEvents(
	ctx context.Context, accountID uuid.UUID, limit int, now, lease time.Time,
) ([]webhooks.Delivery, error) {
	return s.Webhooks.ClaimDueWebhookEvents(ctx, accountID, limit, now, lease)
}

// SetWebhookEventDelivered records a 2xx outcome.
func (s *Store) SetWebhookEventDelivered(
	ctx context.Context, accountID, id uuid.UUID, code int, body string,
) error {
	return s.Webhooks.SetWebhookEventDelivered(ctx, accountID, id, code, body)
}

// SetWebhookEventFailure records a failure outcome.
func (s *Store) SetWebhookEventFailure(
	ctx context.Context, accountID, id uuid.UUID, status domain.WebhookEventStatus,
	code *int, body string, nextAttempt *time.Time,
) error {
	return s.Webhooks.SetWebhookEventFailure(ctx, accountID, id, status, code, body, nextAttempt)
}

// --- jobs runner ---

// TryAdvisoryLock answers whether the caller's transaction won the
// jobs lock.
func (s *Store) TryAdvisoryLock(ctx context.Context) (bool, error) {
	return s.Jobs.TryAdvisoryLock(ctx)
}

// PruneWebhookEvents deletes events older than the cutoff.
func (s *Store) PruneWebhookEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.Jobs.PruneWebhookEvents(ctx, cutoff)
}

// PruneExpiredTokens deletes tokens past their grace window.
func (s *Store) PruneExpiredTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.Jobs.PruneExpiredTokens(ctx, cutoff)
}

// Compile-time proofs that the Store satisfies the ports (the delivery
// worker's port is proven in its own package to keep the import graph
// acyclic).
var (
	_ webhooks.Store          = (*Store)(nil)
	_ webhooks.PublisherStore = (*Store)(nil)
	_ jobs.Store              = (*Store)(nil)
)
