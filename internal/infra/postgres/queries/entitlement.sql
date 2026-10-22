-- Entitlements (internal/app/entitlements) and the policy_entitlements
-- join (internal/app/policies attach/detach).

-- name: GetEntitlementByID :one
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id AND id = @id;

-- name: GetEntitlementByCode :one
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id AND lower(code) = lower(@code);

-- name: InsertEntitlement :exec
INSERT INTO lic.entitlements (id, account_id, name, code, metadata)
VALUES (@id, @account_id, @name, @code, @metadata);

-- name: UpdateEntitlement :exec
UPDATE lic.entitlements SET
  name = @name,
  code = @code,
  metadata = @metadata,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteEntitlement :execrows
DELETE FROM lic.entitlements
WHERE account_id = @account_id AND id = @id;

-- name: ListEntitlementsDesc :many
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT e2.created_at, e2.id FROM lic.entitlements e2
      WHERE e2.account_id = @account_id AND e2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListEntitlementsAsc :many
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT e2.created_at, e2.id FROM lic.entitlements e2
      WHERE e2.account_id = @account_id AND e2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountEntitlements :one
SELECT count(*) FROM lic.entitlements WHERE account_id = @account_id;

-- Authorized user/license scopes are applied before pagination and counts.
-- name: ListEntitlementsByIDsDesc :many
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id AND id = ANY(@ids::uuid[])
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT e2.created_at, e2.id FROM lic.entitlements e2
      WHERE e2.account_id = @account_id AND e2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListEntitlementsByIDsAsc :many
SELECT id, account_id, name, code, metadata, created_at, updated_at
FROM lic.entitlements
WHERE account_id = @account_id AND id = ANY(@ids::uuid[])
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT e2.created_at, e2.id FROM lic.entitlements e2
      WHERE e2.account_id = @account_id AND e2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountEntitlementsByIDs :one
SELECT count(*) FROM lic.entitlements
WHERE account_id = @account_id AND id = ANY(@ids::uuid[]);

-- The entitlements of a user's licenses (owned or attached): the union
-- of policy entitlements and license entitlements.
-- name: UserEntitlementIDs :many
SELECT entitlement_id FROM (
  SELECT pe.entitlement_id
  FROM lic.policy_entitlements pe
  JOIN lic.policies po ON po.id = pe.policy_id
  JOIN lic.licenses l ON l.policy_id = po.id
  WHERE pe.account_id = @account_id
    AND (
      l.user_id = @user_id
      OR EXISTS (
        SELECT 1 FROM lic.license_users lu
        WHERE lu.license_id = l.id AND lu.user_id = @user_id
      )
    )
  UNION
  SELECT le.entitlement_id
  FROM lic.license_entitlements le
  JOIN lic.licenses l ON l.id = le.license_id
  WHERE le.account_id = @account_id
    AND (
      l.user_id = @user_id
      OR EXISTS (
        SELECT 1 FROM lic.license_users lu
        WHERE lu.license_id = l.id AND lu.user_id = @user_id
      )
    )
) ids;

-- The entitlements of one license: its policy's plus its own.
-- name: LicenseEntitlementIDs :many
SELECT entitlement_id FROM (
  SELECT pe.entitlement_id
  FROM lic.policy_entitlements pe
  JOIN lic.policies po ON po.id = pe.policy_id
  JOIN lic.licenses l ON l.policy_id = po.id
  WHERE pe.account_id = @account_id AND l.id = @license_id
  UNION
  SELECT le.entitlement_id
  FROM lic.license_entitlements le
  WHERE le.account_id = @account_id AND le.license_id = @license_id
) ids;

-- policy_entitlements: the attach/detach join rows.

-- name: InsertPolicyEntitlements :execrows
INSERT INTO lic.policy_entitlements (id, account_id, policy_id, entitlement_id)
SELECT @join_id, @account_id, @policy_id, t.entitlement_id
FROM unnest(@entitlement_ids::uuid[]) AS t(entitlement_id);

-- name: ListPolicyEntitlements :many
SELECT pe.id, pe.account_id, pe.policy_id, pe.entitlement_id, pe.created_at, pe.updated_at
FROM lic.policy_entitlements pe
WHERE pe.account_id = @account_id AND pe.policy_id = @policy_id
ORDER BY pe.created_at ASC, pe.id ASC;

-- name: GetPolicyEntitlement :one
SELECT pe.id, pe.account_id, pe.policy_id, pe.entitlement_id, pe.created_at, pe.updated_at
FROM lic.policy_entitlements pe
WHERE pe.account_id = @account_id
  AND pe.policy_id = @policy_id
  AND pe.entitlement_id = @entitlement_id;

-- name: DeletePolicyEntitlements :execrows
DELETE FROM lic.policy_entitlements pe
WHERE pe.account_id = @account_id AND pe.policy_id = @policy_id
  AND pe.entitlement_id IN (SELECT unnest(@entitlement_ids::uuid[]));
