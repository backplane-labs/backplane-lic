-- Webhook endpoints and events (internal/app/webhooks, internal/infra/webhook).

-- name: GetWebhookEndpointByID :one
SELECT id, account_id, product_id, url, subscriptions, signature_algorithm,
  api_version, created_at, updated_at
FROM lic.webhook_endpoints
WHERE account_id = @account_id AND id = @id;

-- name: GetWebhookEndpointProduct :one
-- The event's endpoint's product (the product bearer's read scope).
SELECT product_id FROM lic.webhook_endpoints
WHERE account_id = @account_id AND id = @id;

-- name: GetWebhookEndpointByURL :one
-- The retry action's fallback when the event's endpoint row is gone:
-- the first endpoint of the account with the event's URL snapshot.
SELECT id, account_id, product_id, url, subscriptions, signature_algorithm,
  api_version, created_at, updated_at
FROM lic.webhook_endpoints
WHERE account_id = @account_id AND url = @url
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: InsertWebhookEndpoint :exec
INSERT INTO lic.webhook_endpoints (
  id, account_id, product_id, url, subscriptions, signature_algorithm, api_version
) VALUES (
  @id, @account_id, @product_id, @url, @subscriptions, @signature_algorithm, @api_version
);

-- name: UpdateWebhookEndpoint :exec
UPDATE lic.webhook_endpoints SET
  url = @url,
  subscriptions = @subscriptions,
  signature_algorithm = @signature_algorithm,
  api_version = @api_version,
  product_id = @product_id,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteWebhookEndpoint :exec
DELETE FROM lic.webhook_endpoints WHERE account_id = @account_id AND id = @id;

-- name: DisableWebhookEndpoint :exec
-- Keygen's disable!: subscriptions emptied, saved without validation and
-- without touching updated_at.
UPDATE lic.webhook_endpoints SET subscriptions = '[]'::jsonb
WHERE account_id = @account_id AND id = @id;

-- name: CountWebhookEndpoints :one
SELECT count(*)::int AS total FROM lic.webhook_endpoints
WHERE account_id = @account_id
  AND (@product_id::uuid IS NULL OR product_id = @product_id);

-- name: ListWebhookEndpoints :many
SELECT id, account_id, product_id, url, subscriptions, signature_algorithm,
  api_version, created_at, updated_at
FROM lic.webhook_endpoints
WHERE account_id = @account_id
  AND (@product_id::uuid IS NULL OR product_id = @product_id)
  AND (
    @cursor::uuid IS NULL
    OR CASE
      WHEN @asc::boolean THEN
        (created_at, id) > (
          SELECT c.created_at, c.id FROM lic.webhook_endpoints c WHERE c.id = @cursor
        )
      ELSE
        (created_at, id) < (
          SELECT c.created_at, c.id FROM lic.webhook_endpoints c WHERE c.id = @cursor
        )
    END
  )
ORDER BY
  CASE WHEN @asc::boolean THEN created_at END ASC,
  CASE WHEN @asc::boolean THEN id END ASC,
  created_at DESC,
  id DESC
LIMIT @limit
OFFSET @offset;

-- name: ListSubscribedWebhookEndpoints :many
-- The endpoints an event is delivered to (the publisher's outbox fan-out).
SELECT id, account_id, product_id, url, subscriptions, signature_algorithm,
  api_version, created_at, updated_at
FROM lic.webhook_endpoints
WHERE account_id = @account_id
  AND (subscriptions ? '*' OR subscriptions ? @event::text)
ORDER BY created_at DESC, id DESC;

-- name: InsertWebhookEvent :exec
INSERT INTO lic.webhook_events (
  id, account_id, webhook_endpoint_id, endpoint, event, payload, api_version,
  idempotency_token, status, attempts
) VALUES (
  @id, @account_id, @webhook_endpoint_id, @endpoint, @event, @payload, @api_version,
  @idempotency_token, @status, 0
);

-- name: GetWebhookEventByID :one
SELECT id, account_id, webhook_endpoint_id, endpoint, event, payload, api_version,
  idempotency_token, status, attempts, last_response_code,
  last_response_body, created_at, updated_at
FROM lic.webhook_events
WHERE account_id = @account_id AND id = @id;

-- name: CountWebhookEvents :one
SELECT count(*)::int AS total FROM lic.webhook_events e
LEFT JOIN lic.webhook_endpoints we ON we.id = e.webhook_endpoint_id
WHERE e.account_id = @account_id
  AND (@events::text[] IS NULL OR e.event = ANY(@events::text[]))
  AND (
    @product_id::uuid IS NULL
    OR (e.webhook_endpoint_id IS NOT NULL AND we.product_id = @product_id)
  );

-- name: ListWebhookEvents :many
SELECT e.id, e.account_id, e.webhook_endpoint_id, e.endpoint, e.event, e.payload,
  e.api_version, e.idempotency_token, e.status, e.attempts,
  e.last_response_code, e.last_response_body, e.created_at, e.updated_at
FROM lic.webhook_events e
LEFT JOIN lic.webhook_endpoints we ON we.id = e.webhook_endpoint_id
WHERE e.account_id = @account_id
  AND (@events::text[] IS NULL OR e.event = ANY(@events::text[]))
  AND (
    @product_id::uuid IS NULL
    OR (e.webhook_endpoint_id IS NOT NULL AND we.product_id = @product_id)
  )
  AND (
    @cursor::uuid IS NULL
    OR CASE
      WHEN @asc::boolean THEN
        (e.created_at, e.id) > (
          SELECT c.created_at, c.id FROM lic.webhook_events c WHERE c.id = @cursor
        )
      ELSE
        (e.created_at, e.id) < (
          SELECT c.created_at, c.id FROM lic.webhook_events c WHERE c.id = @cursor
        )
    END
  )
ORDER BY
  CASE WHEN @asc::boolean THEN e.created_at END ASC,
  CASE WHEN @asc::boolean THEN e.id END ASC,
  e.created_at DESC,
  e.id DESC
LIMIT @limit
OFFSET @offset;

-- name: DeleteWebhookEvent :exec
DELETE FROM lic.webhook_events WHERE account_id = @account_id AND id = @id;

-- name: SetWebhookEventDelivered :exec
UPDATE lic.webhook_events SET
  status = 'DELIVERED',
  last_response_code = @last_response_code,
  last_response_body = @last_response_body,
  attempts = attempts + 1,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: SetWebhookEventFailure :exec
UPDATE lic.webhook_events SET
  status = @status,
  last_response_code = @last_response_code,
  last_response_body = @last_response_body,
  attempts = attempts + 1,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: GetWebhookDeliveryMessage :one
-- Delivery diagnostics; ids are shared with the API resource.
SELECT id, status, attempts, next_attempt_at, payload, last_error, finished_at
FROM lic.outbox_messages WHERE id = @id;

-- name: CountWebhookDeliveryMessages :one
SELECT count(*)::bigint AS total FROM lic.outbox_messages
WHERE partition_key = @account_id::text;

-- name: MakeWebhookDeliveryDue :exec
-- Releasing a scheduled delivery is useful for controlled operational replay.
UPDATE lic.outbox_messages SET next_attempt_at = now()
WHERE id = @id AND status = 'pending';

-- name: SetWebhookEventExhausted :exec
-- Relay budget exhausted without an HTTP outcome (e.g. signing failed).
UPDATE lic.webhook_events SET status = 'FAILED', updated_at = now()
WHERE account_id = @account_id AND id = @id;
