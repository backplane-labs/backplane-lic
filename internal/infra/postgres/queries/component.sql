-- Machine components (internal/app/machines): their machine's scope
-- decides who sees them.

-- name: ListComponents :many
SELECT c.id, c.account_id, c.machine_id, c.name, c.fingerprint, c.metadata, c.created_at, c.updated_at,
  l.product_id, m.license_id, count(*) OVER ()::bigint AS total
FROM lic.machine_components c
JOIN lic.machines m ON m.id = c.machine_id
JOIN lic.licenses l ON l.id = m.license_id
WHERE c.account_id = @account_id
  AND (sqlc.narg(component_id)::uuid IS NULL OR c.id = sqlc.narg(component_id))
  AND (sqlc.narg(filter_machine_id)::uuid IS NULL OR c.machine_id = sqlc.narg(filter_machine_id))
  AND (sqlc.narg(filter_product_id)::uuid IS NULL OR l.product_id = sqlc.narg(filter_product_id))
  AND (sqlc.narg(filter_license_id)::uuid IS NULL OR m.license_id = sqlc.narg(filter_license_id))
  AND (sqlc.narg(filter_owner_id)::uuid IS NULL OR m.owner_id = sqlc.narg(filter_owner_id))
  AND (sqlc.narg(filter_user)::text IS NULL OR EXISTS (
    SELECT 1 FROM lic.users u
    WHERE u.account_id = c.account_id AND (u.id::text = sqlc.narg(filter_user) OR u.email = sqlc.narg(filter_user))
      AND (l.user_id = u.id OR EXISTS (
        SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = u.id
      ))
  ))
  AND (sqlc.narg(scope_kind)::text IS NULL
    OR (sqlc.narg(scope_kind)::text = 'product' AND l.product_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'license' AND m.license_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'user' AND (l.user_id = sqlc.narg(scope_id) OR EXISTS (
      SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = sqlc.narg(scope_id)
    ))))
  AND (sqlc.narg(cursor)::uuid IS NULL
    OR (@asc::boolean AND (c.created_at, c.id) > (SELECT x.created_at, x.id FROM lic.machine_components x WHERE x.id = sqlc.narg(cursor)))
    OR (NOT @asc::boolean AND (c.created_at, c.id) < (SELECT x.created_at, x.id FROM lic.machine_components x WHERE x.id = sqlc.narg(cursor))))
ORDER BY
  CASE WHEN @asc::boolean THEN c.created_at END ASC,
  CASE WHEN @asc::boolean THEN c.id END ASC,
  CASE WHEN NOT @asc::boolean THEN c.created_at END DESC,
  CASE WHEN NOT @asc::boolean THEN c.id END DESC
LIMIT @fetch OFFSET @page_offset;

-- name: UpdateMachineComponent :exec
UPDATE lic.machine_components SET name = @name, metadata = @metadata, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: DeleteMachineComponent :exec
DELETE FROM lic.machine_components WHERE account_id = @account_id AND id = @id;

-- name: ComponentFingerprintTaken :one
SELECT EXISTS (
  SELECT 1 FROM lic.machine_components c
  JOIN lic.machines m ON m.id = c.machine_id
  JOIN lic.licenses l ON l.id = m.license_id
  WHERE c.account_id = @account_id AND c.fingerprint = @fingerprint
    AND CASE @kind::text
      WHEN 'account' THEN true
      WHEN 'product' THEN l.product_id = @scope_id
      WHEN 'policy' THEN m.policy_id = @scope_id
      WHEN 'license' THEN m.license_id = @scope_id
      ELSE c.machine_id = @scope_id
    END
);
