package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// WebhooksRepo adapts the webhook endpoint and event queries to the
// webhooks scenario's storage port (and the publisher's).
type WebhooksRepo struct {
	q *db.Queries
}

// WebhookEndpointByID loads an endpoint by id.
func (r WebhooksRepo) WebhookEndpointByID(
	ctx context.Context, accountID, id uuid.UUID,
) (domain.WebhookEndpoint, error) {
	row, err := r.q.GetWebhookEndpointByID(ctx, db.GetWebhookEndpointByIDParams{AccountID: accountID, ID: id})

	return endpointDomain(row.ID, row.AccountID, row.ProductID, row.URL, row.Subscriptions,
		row.SignatureAlgorithm, row.APIVersion, row.CreatedAt, row.UpdatedAt, "id", err)
}

// WebhookEndpointProduct answers the endpoint's product (the product
// bearer's read scope); false when the endpoint row is gone.
func (r WebhooksRepo) WebhookEndpointProduct(
	ctx context.Context, accountID, id uuid.UUID,
) (uuid.NullUUID, error) {
	row, err := r.q.GetWebhookEndpointProduct(ctx, db.GetWebhookEndpointProductParams{
		AccountID: accountID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}, fmt.Errorf(
			"postgres: webhook endpoint %s product: %w", id, webhooks.ErrNotFound)
	}

	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("postgres: webhook endpoint %s product: %w", id, err)
	}

	if row.ProductID == nil {
		return uuid.NullUUID{}, nil
	}

	return uuid.NullUUID{UUID: *row.ProductID, Valid: true}, nil
}

// WebhookEndpointByURL loads the first endpoint with the URL (the retry
// action's fallback when the event's endpoint row is gone).
func (r WebhooksRepo) WebhookEndpointByURL(
	ctx context.Context, accountID uuid.UUID, url string,
) (domain.WebhookEndpoint, error) {
	row, err := r.q.GetWebhookEndpointByURL(ctx, db.GetWebhookEndpointByURLParams{AccountID: accountID, URL: url})

	return endpointDomain(row.ID, row.AccountID, row.ProductID, row.URL, row.Subscriptions,
		row.SignatureAlgorithm, row.APIVersion, row.CreatedAt, row.UpdatedAt, "url", err)
}

// InsertWebhookEndpoint writes a new endpoint row.
func (r WebhooksRepo) InsertWebhookEndpoint(ctx context.Context, e domain.WebhookEndpoint) error {
	err := r.q.InsertWebhookEndpoint(ctx, db.InsertWebhookEndpointParams{
		ID:                 e.ID,
		AccountID:          e.AccountID,
		ProductID:          nullUUIDPtr(e.ProductID),
		URL:                e.URL,
		Subscriptions:      subscriptionsJSON(e.Subscriptions),
		SignatureAlgorithm: e.SignatureAlgorithm,
		APIVersion:         &e.APIVersion,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert webhook endpoint %s: %w", e.ID, err)
	}

	return nil
}

// UpdateWebhookEndpoint writes every mutable column.
func (r WebhooksRepo) UpdateWebhookEndpoint(ctx context.Context, e domain.WebhookEndpoint) error {
	err := r.q.UpdateWebhookEndpoint(ctx, db.UpdateWebhookEndpointParams{
		URL:                e.URL,
		Subscriptions:      subscriptionsJSON(e.Subscriptions),
		SignatureAlgorithm: e.SignatureAlgorithm,
		APIVersion:         &e.APIVersion,
		ProductID:          nullUUIDPtr(e.ProductID),
		AccountID:          e.AccountID,
		ID:                 e.ID,
	})
	if err != nil {
		return fmt.Errorf("postgres: update webhook endpoint %s: %w", e.ID, err)
	}

	return nil
}

// DeleteWebhookEndpoint removes the endpoint.
func (r WebhooksRepo) DeleteWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteWebhookEndpoint(
		ctx, db.DeleteWebhookEndpointParams{AccountID: accountID, ID: id},
	); err != nil {
		return fmt.Errorf("postgres: delete webhook endpoint %s: %w", id, err)
	}

	return nil
}

// DisableWebhookEndpoint empties the subscriptions (auto-disable).
func (r WebhooksRepo) DisableWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DisableWebhookEndpoint(
		ctx, db.DisableWebhookEndpointParams{AccountID: accountID, ID: id},
	); err != nil {
		return fmt.Errorf("postgres: disable webhook endpoint %s: %w", id, err)
	}

	return nil
}

// ListWebhookEndpoints answers the (product-scoped) page.
func (r WebhooksRepo) ListWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, product uuid.NullUUID, p paging.Params,
) ([]domain.WebhookEndpoint, error) {
	rows, err := r.q.ListWebhookEndpoints(ctx, db.ListWebhookEndpointsParams{
		AccountID: accountID,
		ProductID: nullUUIDPtr(product),
		Cursor:    cursorPtr(p.Cursor),
		Asc:       p.Order == paging.OrderAsc,
		Limit:     int64(p.SizeOrDefault()),
		Offset:    int64(offsetOf(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list webhook endpoints: %w", err)
	}

	out := make([]domain.WebhookEndpoint, 0, len(rows))
	for i := range rows {
		endpoint, err := endpointOf(rows[i].ID, rows[i].AccountID, rows[i].ProductID, rows[i].URL,
			rows[i].Subscriptions, rows[i].SignatureAlgorithm, rows[i].APIVersion,
			rows[i].CreatedAt, rows[i].UpdatedAt)
		if err != nil {
			return nil, err
		}

		out = append(out, endpoint)
	}

	return out, nil
}

// CountWebhookEndpoints answers the scoped total.
func (r WebhooksRepo) CountWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, product uuid.NullUUID,
) (int, error) {
	row, err := r.q.CountWebhookEndpoints(ctx, db.CountWebhookEndpointsParams{
		AccountID: accountID, ProductID: nullUUIDPtr(product),
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count webhook endpoints: %w", err)
	}

	return int(row.Total), nil
}

// SubscribedWebhookEndpoints answers the endpoints an event goes to
// (the publisher's fan-out candidates).
func (r WebhooksRepo) SubscribedWebhookEndpoints(
	ctx context.Context, accountID uuid.UUID, event string,
) ([]domain.WebhookEndpoint, error) {
	rows, err := r.q.ListSubscribedWebhookEndpoints(ctx, db.ListSubscribedWebhookEndpointsParams{
		AccountID: accountID, Event: event,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: subscribed webhook endpoints: %w", err)
	}

	out := make([]domain.WebhookEndpoint, 0, len(rows))
	for i := range rows {
		endpoint, err := endpointOf(rows[i].ID, rows[i].AccountID, rows[i].ProductID, rows[i].URL,
			rows[i].Subscriptions, rows[i].SignatureAlgorithm, rows[i].APIVersion,
			rows[i].CreatedAt, rows[i].UpdatedAt)
		if err != nil {
			return nil, err
		}

		out = append(out, endpoint)
	}

	return out, nil
}

// InsertWebhookEvent writes an outbox row (inside the caller's
// transaction when the context carries one).
func (r WebhooksRepo) InsertWebhookEvent(ctx context.Context, e domain.WebhookEvent) error {
	err := r.q.InsertWebhookEvent(ctx, db.InsertWebhookEventParams{
		ID:                e.ID,
		AccountID:         e.AccountID,
		WebhookEndpointID: nullUUIDPtr(e.EndpointID),
		Endpoint:          e.Endpoint,
		Event:             e.Event,
		Payload:           e.Payload,
		APIVersion:        &e.APIVersion,
		IdempotencyToken:  e.IdempotencyToken,
		Status:            string(e.Status),
		NextAttemptAt:     e.NextAttemptAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert webhook event %s: %w", e.ID, err)
	}

	return nil
}

// WebhookEventByID loads an event by id.
func (r WebhooksRepo) WebhookEventByID(
	ctx context.Context, accountID, id uuid.UUID,
) (domain.WebhookEvent, error) {
	row, err := r.q.GetWebhookEventByID(ctx, db.GetWebhookEventByIDParams{AccountID: accountID, ID: id})

	return eventDomain(row.ID, row.AccountID, row.WebhookEndpointID, row.Endpoint, row.Event,
		row.Payload, row.APIVersion, row.IdempotencyToken, row.Status, row.Attempts,
		row.NextAttemptAt, row.LastResponseCode, row.LastResponseBody, row.CreatedAt,
		row.UpdatedAt, "id", err)
}

// ListWebhookEvents answers the filtered page.
func (r WebhooksRepo) ListWebhookEvents(
	ctx context.Context, accountID uuid.UUID, f webhooks.EventFilter, product uuid.NullUUID,
	p paging.Params,
) ([]domain.WebhookEvent, error) {
	var events []string
	if len(f.Events) > 0 {
		events = f.Events
	}

	rows, err := r.q.ListWebhookEvents(ctx, db.ListWebhookEventsParams{
		AccountID: accountID,
		Events:    events,
		ProductID: nullUUIDPtr(product),
		Cursor:    cursorPtr(p.Cursor),
		Asc:       p.Order == paging.OrderAsc,
		Limit:     int64(p.SizeOrDefault()),
		Offset:    int64(offsetOf(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list webhook events: %w", err)
	}

	out := make([]domain.WebhookEvent, 0, len(rows))

	for i := range rows {
		row := rows[i]

		event, err := eventDomain(row.ID, row.AccountID, row.WebhookEndpointID, row.Endpoint,
			row.Event, row.Payload, row.APIVersion, row.IdempotencyToken, row.Status, row.Attempts,
			row.NextAttemptAt, row.LastResponseCode, row.LastResponseBody, row.CreatedAt,
			row.UpdatedAt, "", nil)
		if err != nil {
			return nil, err
		}

		out = append(out, event)
	}

	return out, nil
}

// CountWebhookEvents answers the filtered total.
func (r WebhooksRepo) CountWebhookEvents(
	ctx context.Context, accountID uuid.UUID, f webhooks.EventFilter, product uuid.NullUUID,
) (int, error) {
	var events []string
	if len(f.Events) > 0 {
		events = f.Events
	}

	row, err := r.q.CountWebhookEvents(ctx, db.CountWebhookEventsParams{
		AccountID: accountID, Events: events, ProductID: nullUUIDPtr(product),
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count webhook events: %w", err)
	}

	return int(row.Total), nil
}

// DeleteWebhookEvent removes the event.
func (r WebhooksRepo) DeleteWebhookEvent(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteWebhookEvent(
		ctx, db.DeleteWebhookEventParams{AccountID: accountID, ID: id},
	); err != nil {
		return fmt.Errorf("postgres: delete webhook event %s: %w", id, err)
	}

	return nil
}

// --- worker face ---

// ClaimDueWebhookEvents claims the due batch and pushes it to the
// lease horizon in one short transaction (FOR UPDATE SKIP LOCKED); the
// deliveries run outside any transaction.
func (r WebhooksRepo) ClaimDueWebhookEvents(
	ctx context.Context, accountID uuid.UUID, limit int, now, lease time.Time,
) ([]webhooks.Delivery, error) {
	rows, err := r.q.ClaimDueWebhookEvents(ctx, db.ClaimDueWebhookEventsParams{
		AccountID: accountID, Now: &now, Limit: int64(limit), Lease: &lease,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: claim webhook events: %w", err)
	}

	out := make([]webhooks.Delivery, 0, len(rows))

	for i := range rows {
		row := rows[i]

		event, err := eventDomain(row.ID, row.AccountID, row.WebhookEndpointID, row.Endpoint,
			row.Event, row.Payload, row.APIVersion, row.IdempotencyToken, row.Status, row.Attempts,
			row.NextAttemptAt, row.LastResponseCode, row.LastResponseBody, row.CreatedAt,
			row.UpdatedAt, "", nil)
		if err != nil {
			return nil, err
		}

		out = append(out, webhooks.Delivery{
			Event:              event,
			EndpointURL:        row.EndpointURL,
			SignatureAlgorithm: row.EndpointSignatureAlgorithm,
		})
	}

	return out, nil
}

// SetWebhookEventDelivered records a 2xx outcome.
func (r WebhooksRepo) SetWebhookEventDelivered(
	ctx context.Context, accountID, id uuid.UUID, code int, body string,
) error {
	code32 := int32(code) //nolint:gosec // HTTP status codes fit int32

	err := r.q.SetWebhookEventDelivered(ctx, db.SetWebhookEventDeliveredParams{
		AccountID:        accountID,
		ID:               id,
		LastResponseCode: &code32,
		LastResponseBody: &body,
	})
	if err != nil {
		return fmt.Errorf("postgres: mark webhook event %s delivered: %w", id, err)
	}

	return nil
}

// SetWebhookEventFailure records a non-2xx or transport outcome.
func (r WebhooksRepo) SetWebhookEventFailure(
	ctx context.Context, accountID, id uuid.UUID, status domain.WebhookEventStatus,
	code *int, body string, nextAttempt *time.Time,
) error {
	var code32 *int32

	if code != nil {
		value := int32(*code) //nolint:gosec // HTTP status codes fit int32
		code32 = &value
	}

	err := r.q.SetWebhookEventFailure(ctx, db.SetWebhookEventFailureParams{
		AccountID:        accountID,
		ID:               id,
		Status:           string(status),
		LastResponseCode: code32,
		LastResponseBody: &body,
		NextAttemptAt:    nextAttempt,
	})
	if err != nil {
		return fmt.Errorf("postgres: mark webhook event %s failed: %w", id, err)
	}

	return nil
}

// --- row mapping ---

func endpointDomain(
	id uuid.UUID, accountID uuid.UUID, productID *uuid.UUID, url string,
	subscriptions json.RawMessage, signatureAlgorithm string, apiVersion *string,
	createdAt, updatedAt time.Time, by string, err error,
) (domain.WebhookEndpoint, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WebhookEndpoint{}, fmt.Errorf(
			"postgres: webhook endpoint %s by %s: %w", id, by, webhooks.ErrNotFound)
	}

	if err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("postgres: webhook endpoint %s: %w", id, err)
	}

	return endpointOf(id, accountID, productID, url, subscriptions, signatureAlgorithm,
		apiVersion, createdAt, updatedAt)
}

func endpointOf(
	id uuid.UUID, accountID uuid.UUID, productID *uuid.UUID, url string,
	subscriptions json.RawMessage, signatureAlgorithm string, apiVersion *string,
	createdAt, updatedAt time.Time,
) (domain.WebhookEndpoint, error) {
	var subs []string
	if err := json.Unmarshal(subscriptions, &subs); err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("postgres: webhook endpoint %s: subscriptions: %w", id, err)
	}

	version := domain.WebhookEndpoint{}.APIVersion
	if apiVersion != nil {
		version = *apiVersion
	}

	endpoint := domain.WebhookEndpoint{
		ID: id, AccountID: accountID, URL: url, Subscriptions: subs,
		SignatureAlgorithm: signatureAlgorithm, APIVersion: version,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if productID != nil {
		endpoint.ProductID = uuid.NullUUID{UUID: *productID, Valid: true}
	}

	return endpoint, nil
}

func eventDomain(
	id, accountID uuid.UUID, endpointID *uuid.UUID, endpoint, event, payload string,
	apiVersion *string, idempotencyToken, status string, attempts int32,
	nextAttemptAt *time.Time, lastResponseCode *int32, lastResponseBody *string,
	createdAt, updatedAt time.Time, by string, err error,
) (domain.WebhookEvent, error) {
	if by != "" && errors.Is(err, pgx.ErrNoRows) {
		return domain.WebhookEvent{}, fmt.Errorf(
			"postgres: webhook event %s by %s: %w", id, by, webhooks.ErrNotFound)
	}

	if err != nil {
		return domain.WebhookEvent{}, fmt.Errorf("postgres: webhook event %s: %w", id, err)
	}

	version := ""
	if apiVersion != nil {
		version = *apiVersion
	}

	out := domain.WebhookEvent{
		ID: id, AccountID: accountID, Endpoint: endpoint, Event: event, Payload: payload,
		APIVersion: version, IdempotencyToken: idempotencyToken,
		Status: domain.WebhookEventStatus(status), Attempts: int(attempts),
		NextAttemptAt: nextAttemptAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if endpointID != nil {
		out.EndpointID = uuid.NullUUID{UUID: *endpointID, Valid: true}
	}

	if lastResponseCode != nil {
		code := int(*lastResponseCode)
		out.LastResponseCode = &code
	}

	if lastResponseBody != nil {
		out.LastResponseBody = lastResponseBody
	}

	return out, nil
}

func subscriptionsJSON(subs []string) json.RawMessage {
	raw, err := json.Marshal(subs)
	if err != nil {
		// Strings always marshal.
		return json.RawMessage(`["*"]`)
	}

	return raw
}
