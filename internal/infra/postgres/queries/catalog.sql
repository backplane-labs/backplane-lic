-- The declared catalog (internal/app/catalog): its reconcile lock and the
-- reads that find what the configuration does not declare.

-- name: LockCatalog :exec
SELECT pg_advisory_xact_lock(hashtextextended(@account_id::text || ':lic:catalog', 0));

-- name: ListCatalogProductIDs :many
SELECT id FROM lic.products WHERE account_id = @account_id ORDER BY id;

-- name: ListCatalogPolicyIDs :many
SELECT id FROM lic.policies WHERE account_id = @account_id ORDER BY id;

-- name: ListCatalogProductTokens :many
SELECT id, bearer_id, name, digest, permissions, created_at
FROM lic.tokens
WHERE account_id = @account_id AND bearer_type = 'Product'
ORDER BY created_at, id;

-- name: SetDeclaredToken :exec
UPDATE lic.tokens
SET digest = @digest,
    permissions = @permissions,
    updated_at = now()
WHERE account_id = @account_id AND id = @id;
