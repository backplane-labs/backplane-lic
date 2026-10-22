-- Product relationship routes (internal/app/products): the product's
-- users (via its licenses) and its tokens (bearer products).

-- name: ListProductUsersDesc :many
SELECT DISTINCT u.id, u.account_id, u.first_name, u.last_name, u.email,
  u.password_digest, u.role, u.permissions, u.group_id, u.metadata,
  u.banned_at, u.created_at, u.updated_at
FROM lic.users u
JOIN lic.licenses l ON l.account_id = @account_id AND (
  l.user_id = u.id
  OR EXISTS (
    SELECT 1 FROM lic.license_users lu
    WHERE lu.license_id = l.id AND lu.user_id = u.id
  )
)
JOIN lic.policies po ON po.account_id = @account_id AND po.id = l.policy_id
WHERE u.account_id = @account_id
  AND po.product_id = @product_id
  AND u.role = ANY(@roles::text[])
  AND (
    @cursor_id::uuid IS NULL
    OR (u.created_at, u.id) < (
      SELECT u2.created_at, u2.id FROM lic.users u2
      WHERE u2.account_id = @account_id AND u2.id = @cursor_id
    )
  )
ORDER BY u.created_at DESC, u.id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductUsersAsc :many
SELECT DISTINCT u.id, u.account_id, u.first_name, u.last_name, u.email,
  u.password_digest, u.role, u.permissions, u.group_id, u.metadata,
  u.banned_at, u.created_at, u.updated_at
FROM lic.users u
JOIN lic.licenses l ON l.account_id = @account_id AND (
  l.user_id = u.id
  OR EXISTS (
    SELECT 1 FROM lic.license_users lu
    WHERE lu.license_id = l.id AND lu.user_id = u.id
  )
)
JOIN lic.policies po ON po.account_id = @account_id AND po.id = l.policy_id
WHERE u.account_id = @account_id
  AND po.product_id = @product_id
  AND u.role = ANY(@roles::text[])
  AND (
    @cursor_id::uuid IS NULL
    OR (u.created_at, u.id) > (
      SELECT u2.created_at, u2.id FROM lic.users u2
      WHERE u2.account_id = @account_id AND u2.id = @cursor_id
    )
  )
ORDER BY u.created_at ASC, u.id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProductUsers :one
SELECT count(DISTINCT u.id)
FROM lic.users u
JOIN lic.licenses l ON l.account_id = @account_id AND (
  l.user_id = u.id
  OR EXISTS (
    SELECT 1 FROM lic.license_users lu
    WHERE lu.license_id = l.id AND lu.user_id = u.id
  )
)
JOIN lic.policies po ON po.account_id = @account_id AND po.id = l.policy_id
WHERE u.account_id = @account_id
  AND po.product_id = @product_id
  AND u.role = ANY(@roles::text[]);

-- name: GetProductUserByID :one
SELECT DISTINCT u.id, u.account_id, u.first_name, u.last_name, u.email,
  u.password_digest, u.role, u.permissions, u.group_id, u.metadata,
  u.banned_at, u.created_at, u.updated_at
FROM lic.users u
JOIN lic.licenses l ON l.account_id = @account_id AND (
  l.user_id = u.id
  OR EXISTS (
    SELECT 1 FROM lic.license_users lu
    WHERE lu.license_id = l.id AND lu.user_id = u.id
  )
)
JOIN lic.policies po ON po.account_id = @account_id AND po.id = l.policy_id
WHERE u.account_id = @account_id
  AND po.product_id = @product_id
  AND u.role = ANY(@roles::text[])
  AND u.id = @user_id;

-- name: GetProductUserByEmail :one
SELECT DISTINCT u.id, u.account_id, u.first_name, u.last_name, u.email,
  u.password_digest, u.role, u.permissions, u.group_id, u.metadata,
  u.banned_at, u.created_at, u.updated_at
FROM lic.users u
JOIN lic.licenses l ON l.account_id = @account_id AND (
  l.user_id = u.id
  OR EXISTS (
    SELECT 1 FROM lic.license_users lu
    WHERE lu.license_id = l.id AND lu.user_id = u.id
  )
)
JOIN lic.policies po ON po.account_id = @account_id AND po.id = l.policy_id
WHERE u.account_id = @account_id
  AND po.product_id = @product_id
  AND u.role = ANY(@roles::text[])
  AND lower(u.email) = lower(@email);

-- name: ListProductTokensDesc :many
SELECT t.id, t.account_id, t.digest, t.bearer_id, t.bearer_type, t.bearer_role,
  t.kind, t.name, t.expiry, t.permissions, t.created_at, t.updated_at
FROM lic.tokens t
WHERE t.account_id = @account_id
  AND t.bearer_type = 'Product' AND t.bearer_id = @product_id
  AND (
    @cursor_id::uuid IS NULL
    OR (t.created_at, t.id) < (
      SELECT t2.created_at, t2.id FROM lic.tokens t2
      WHERE t2.account_id = @account_id AND t2.id = @cursor_id
    )
  )
ORDER BY t.created_at DESC, t.id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductTokensAsc :many
SELECT t.id, t.account_id, t.digest, t.bearer_id, t.bearer_type, t.bearer_role,
  t.kind, t.name, t.expiry, t.permissions, t.created_at, t.updated_at
FROM lic.tokens t
WHERE t.account_id = @account_id
  AND t.bearer_type = 'Product' AND t.bearer_id = @product_id
  AND (
    @cursor_id::uuid IS NULL
    OR (t.created_at, t.id) > (
      SELECT t2.created_at, t2.id FROM lic.tokens t2
      WHERE t2.account_id = @account_id AND t2.id = @cursor_id
    )
  )
ORDER BY t.created_at ASC, t.id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProductTokens :one
SELECT count(*) FROM lic.tokens
WHERE account_id = @account_id AND bearer_type = 'Product' AND bearer_id = @product_id;

-- name: GetProductToken :one
SELECT t.id, t.account_id, t.digest, t.bearer_id, t.bearer_type, t.bearer_role,
  t.kind, t.name, t.expiry, t.permissions, t.created_at, t.updated_at
FROM lic.tokens t
WHERE t.account_id = @account_id
  AND t.bearer_type = 'Product' AND t.bearer_id = @product_id
  AND t.id = @token_id;
