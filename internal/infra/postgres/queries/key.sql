-- Keys (internal/app/keys): the policy key pool's members.

-- name: GetKeyByID :one
SELECT k.id, k.account_id, k.policy_id, po.product_id, k.key,
  k.created_at, k.updated_at
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id AND k.id = @id;

-- name: InsertKey :exec
INSERT INTO lic.keys (id, account_id, policy_id, key)
VALUES (@id, @account_id, @policy_id, @key);

-- name: UpdateKey :exec
UPDATE lic.keys SET key = @key, updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteKey :execrows
DELETE FROM lic.keys
WHERE account_id = @account_id AND id = @id;

-- name: ListKeysDesc :many
SELECT k.id, k.account_id, k.policy_id, po.product_id, k.key,
  k.created_at, k.updated_at
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id
  AND (@filter_policy_id::uuid IS NULL OR k.policy_id = @filter_policy_id)
  AND (@filter_product_id::uuid IS NULL OR po.product_id = @filter_product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (k.created_at, k.id) < (
      SELECT k2.created_at, k2.id FROM lic.keys k2
      WHERE k2.account_id = @account_id AND k2.id = @cursor_id
    )
  )
ORDER BY k.created_at DESC, k.id DESC
LIMIT @limit OFFSET @offset;

-- name: ListKeysAsc :many
SELECT k.id, k.account_id, k.policy_id, po.product_id, k.key,
  k.created_at, k.updated_at
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id
  AND (@filter_policy_id::uuid IS NULL OR k.policy_id = @filter_policy_id)
  AND (@filter_product_id::uuid IS NULL OR po.product_id = @filter_product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (k.created_at, k.id) > (
      SELECT k2.created_at, k2.id FROM lic.keys k2
      WHERE k2.account_id = @account_id AND k2.id = @cursor_id
    )
  )
ORDER BY k.created_at ASC, k.id ASC
LIMIT @limit OFFSET @offset;

-- name: CountKeys :one
SELECT count(*)
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id
  AND (@filter_policy_id::uuid IS NULL OR k.policy_id = @filter_policy_id)
  AND (@filter_product_id::uuid IS NULL OR po.product_id = @filter_product_id);

-- The pool of one policy (ordered like every list).
-- name: ListPolicyPoolDesc :many
SELECT k.id, k.account_id, k.policy_id, po.product_id, k.key,
  k.created_at, k.updated_at
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id AND k.policy_id = @policy_id
  AND (
    @cursor_id::uuid IS NULL
    OR (k.created_at, k.id) < (
      SELECT k2.created_at, k2.id FROM lic.keys k2
      WHERE k2.account_id = @account_id AND k2.id = @cursor_id
    )
  )
ORDER BY k.created_at DESC, k.id DESC
LIMIT @limit OFFSET @offset;

-- name: ListPolicyPoolAsc :many
SELECT k.id, k.account_id, k.policy_id, po.product_id, k.key,
  k.created_at, k.updated_at
FROM lic.keys k
JOIN lic.policies po ON po.id = k.policy_id
WHERE k.account_id = @account_id AND k.policy_id = @policy_id
  AND (
    @cursor_id::uuid IS NULL
    OR (k.created_at, k.id) > (
      SELECT k2.created_at, k2.id FROM lic.keys k2
      WHERE k2.account_id = @account_id AND k2.id = @cursor_id
    )
  )
ORDER BY k.created_at ASC, k.id ASC
LIMIT @limit OFFSET @offset;

-- name: CountPolicyPool :one
SELECT count(*) FROM lic.keys
WHERE account_id = @account_id AND policy_id = @policy_id;

-- Pop: destroy the newest pooled key and answer it.
-- name: PopPolicyKey :one
DELETE FROM lic.keys
WHERE id = (
  SELECT id FROM lic.keys
  WHERE account_id = @account_id AND policy_id = @policy_id
  ORDER BY created_at DESC, id DESC
  LIMIT 1
)
RETURNING id, account_id, policy_id, key, created_at, updated_at;

-- Key conflicts with license identifiers (Key#validate_key_conflicts).
-- name: KeyIsLicenseID :one
SELECT EXISTS (
  SELECT 1 FROM lic.licenses l
  WHERE l.account_id = @account_id AND l.id::text = @key
);

-- name: KeyIsLicenseKey :one
SELECT EXISTS (
  SELECT 1 FROM lic.licenses l
  WHERE l.account_id = @account_id AND l.key = @key
);
