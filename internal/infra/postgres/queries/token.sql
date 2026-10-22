-- Tokens (internal/app/authn, internal/app/tokens).

-- name: GetTokenByDigest :one
SELECT id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions, max_activations, activations, max_deactivations,
  deactivations, created_at, updated_at
FROM lic.tokens
WHERE account_id = @account_id AND digest = @digest;

-- name: GetTokenByID :one
SELECT id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions, max_activations, activations, max_deactivations,
  deactivations, created_at, updated_at
FROM lic.tokens
WHERE account_id = @account_id AND id = @id;

-- name: InsertToken :exec
INSERT INTO lic.tokens (
  id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions, max_activations, max_deactivations
) VALUES (
  @id, @account_id, @digest, @bearer_id, @bearer_type, @bearer_role, @kind,
  @name, @expiry, @permissions, @max_activations, @max_deactivations
);

-- name: RegenerateToken :exec
UPDATE lic.tokens
SET digest = @digest,
    expiry = @expiry,
    updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteToken :exec
DELETE FROM lic.tokens WHERE account_id = @account_id AND id = @id;

-- name: CountTokens :one
SELECT count(*)::int AS total FROM lic.tokens t
WHERE t.account_id = @account_id
  AND (@bearer_type::text IS NULL OR t.bearer_type = @bearer_type)
  AND (@bearer_id::uuid IS NULL OR t.bearer_id = @bearer_id)
  AND (@bearer_roles::text[] IS NULL OR t.bearer_role = ANY(@bearer_roles::text[]))
  AND (
    -- The principal's index scope: no scope (admin family), the bearer's
    -- own tokens (user/license), or the product scope (own product's,
    -- its licenses' and its users' tokens).
    @scope_kind::text IS NULL
    OR (
      @scope_kind::text = 'user'
      AND t.bearer_type = 'User'
      AND t.bearer_id = @scope_id
    )
    OR (
      @scope_kind::text = 'license'
      AND t.bearer_type = 'License'
      AND t.bearer_id = @scope_id
    )
    OR (
      @scope_kind::text = 'product'
      AND (
        (t.bearer_type = 'Product' AND t.bearer_id = @scope_id)
        OR (
          t.bearer_type = 'License'
          AND EXISTS (
            SELECT 1 FROM lic.licenses l WHERE l.id = t.bearer_id AND l.product_id = @scope_id
          )
        )
        OR (
          t.bearer_type = 'User'
          AND EXISTS (
            SELECT 1
            FROM lic.licenses l
            WHERE l.product_id = @scope_id
              AND (l.user_id = t.bearer_id OR EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = t.bearer_id
              ))
          )
        )
      )
    )
  );

-- name: ListTokens :many
SELECT id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions, max_activations, activations, max_deactivations,
  deactivations, created_at, updated_at
FROM lic.tokens t
WHERE t.account_id = @account_id
  AND (@bearer_type::text IS NULL OR t.bearer_type = @bearer_type)
  AND (@bearer_id::uuid IS NULL OR t.bearer_id = @bearer_id)
  AND (@bearer_roles::text[] IS NULL OR t.bearer_role = ANY(@bearer_roles::text[]))
  AND (
    @scope_kind::text IS NULL
    OR (
      @scope_kind::text = 'user'
      AND t.bearer_type = 'User'
      AND t.bearer_id = @scope_id
    )
    OR (
      @scope_kind::text = 'license'
      AND t.bearer_type = 'License'
      AND t.bearer_id = @scope_id
    )
    OR (
      @scope_kind::text = 'product'
      AND (
        (t.bearer_type = 'Product' AND t.bearer_id = @scope_id)
        OR (
          t.bearer_type = 'License'
          AND EXISTS (
            SELECT 1 FROM lic.licenses l WHERE l.id = t.bearer_id AND l.product_id = @scope_id
          )
        )
        OR (
          t.bearer_type = 'User'
          AND EXISTS (
            SELECT 1
            FROM lic.licenses l
            WHERE l.product_id = @scope_id
              AND (l.user_id = t.bearer_id OR EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = t.bearer_id
              ))
          )
        )
      )
    )
  )
  AND (
    @user_bearer::uuid IS NULL
    OR (t.bearer_type = 'User' AND t.bearer_id = @user_bearer)
  )
  AND (
    @cursor::uuid IS NULL
    OR CASE
      WHEN @asc::boolean THEN
        (t.created_at, t.id) > (SELECT c.created_at, c.id FROM lic.tokens c WHERE c.id = @cursor)
      ELSE
        (t.created_at, t.id) < (SELECT c.created_at, c.id FROM lic.tokens c WHERE c.id = @cursor)
    END
  )
ORDER BY
  CASE WHEN @asc::boolean THEN t.created_at END ASC,
  CASE WHEN @asc::boolean THEN t.id END ASC,
  t.created_at DESC,
  t.id DESC
LIMIT @limit
OFFSET @offset;

-- name: UserLicensedToProduct :one
SELECT EXISTS (
  SELECT 1
  FROM lic.licenses l
  WHERE l.account_id = @account_id AND l.product_id = @product_id
    AND (
      l.user_id = @user_id
      OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @user_id)
    )
) AS licensed;

-- name: LicenseOfProduct :one
SELECT EXISTS (
  SELECT 1
  FROM lic.licenses l
  WHERE l.account_id = @account_id AND l.id = @license_id AND l.product_id = @product_id
) AS owned;
