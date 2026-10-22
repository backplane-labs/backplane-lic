-- Products (internal/app/products).
--
-- The list queries carry the three pagination modes as parameters:
-- @cursor_id NULL in limit/offset mode (the row comparison falls away),
-- @offset 0 unless offset mode asked for a later page. The cursor
-- predicate is a strict row comparison against the cursor row; a cursor
-- that does not exist (or belongs to another account) answers an empty
-- page, exactly like the reference's keyset pagination.

-- name: GetProductByID :one
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id AND id = @id;

-- name: GetProductByCode :one
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id AND lower(code) = lower(@code);

-- name: InsertProduct :exec
INSERT INTO lic.products (
  id, account_id, name, code, distribution_strategy, url, platforms, metadata
) VALUES (
  @id, @account_id, @name, @code, @distribution_strategy, @url, @platforms, @metadata
);

-- name: UpdateProduct :exec
UPDATE lic.products SET
  name = @name,
  code = @code,
  distribution_strategy = @distribution_strategy,
  url = @url,
  platforms = @platforms,
  metadata = @metadata,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteProduct :execrows
DELETE FROM lic.products
WHERE account_id = @account_id AND id = @id;

-- The admin-side scope: every product of the account.
-- name: ListProductsDesc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductsAsc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProducts :one
SELECT count(*) FROM lic.products WHERE account_id = @account_id;

-- The user scope (Product.for_user): open products UNION the products of
-- the user's licenses (owned or attached).
-- name: ListProductsForUserDesc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (
    distribution_strategy = 'OPEN'
    OR id IN (
      SELECT p3.id FROM lic.products p3
      JOIN lic.policies po ON po.product_id = p3.id
      JOIN lic.licenses l ON l.policy_id = po.id
      WHERE l.account_id = @account_id
        AND (
          l.user_id = @user_id
          OR EXISTS (
            SELECT 1 FROM lic.license_users lu
            WHERE lu.license_id = l.id AND lu.user_id = @user_id
          )
        )
    )
  )
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductsForUserAsc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (
    distribution_strategy = 'OPEN'
    OR id IN (
      SELECT p3.id FROM lic.products p3
      JOIN lic.policies po ON po.product_id = p3.id
      JOIN lic.licenses l ON l.policy_id = po.id
      WHERE l.account_id = @account_id
        AND (
          l.user_id = @user_id
          OR EXISTS (
            SELECT 1 FROM lic.license_users lu
            WHERE lu.license_id = l.id AND lu.user_id = @user_id
          )
        )
    )
  )
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProductsForUser :one
SELECT count(*) FROM lic.products
WHERE account_id = @account_id
  AND (
    distribution_strategy = 'OPEN'
    OR id IN (
      SELECT p3.id FROM lic.products p3
      JOIN lic.policies po ON po.product_id = p3.id
      JOIN lic.licenses l ON l.policy_id = po.id
      WHERE l.account_id = @account_id
        AND (
          l.user_id = @user_id
          OR EXISTS (
            SELECT 1 FROM lic.license_users lu
            WHERE lu.license_id = l.id AND lu.user_id = @user_id
          )
        )
    )
  );

-- The license scope (Product.for_license): open products UNION the
-- license's own product.
-- name: ListProductsForLicenseDesc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (distribution_strategy = 'OPEN' OR id = @product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductsForLicenseAsc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id
  AND (distribution_strategy = 'OPEN' OR id = @product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProductsForLicense :one
SELECT count(*) FROM lic.products
WHERE account_id = @account_id
  AND (distribution_strategy = 'OPEN' OR id = @product_id);

-- The product-bearer scope (Product.for_product): itself.
-- name: ListProductsForProductDesc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id AND id = @bearer_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListProductsForProductAsc :many
SELECT id, account_id, name, code, distribution_strategy, url, platforms,
  metadata, created_at, updated_at
FROM lic.products
WHERE account_id = @account_id AND id = @bearer_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.products p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountProductsForProduct :one
SELECT count(*) FROM lic.products
WHERE account_id = @account_id AND id = @bearer_id;

-- The license bearer's own product (through its policy).
-- name: GetLicenseProductID :one
SELECT po.product_id
FROM lic.policies po
JOIN lic.licenses l ON l.policy_id = po.id
WHERE l.account_id = @account_id AND l.id = @license_id;
