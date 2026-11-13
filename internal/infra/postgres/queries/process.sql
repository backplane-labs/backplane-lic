-- Machine processes (internal/app/machines): their machine's scope
-- decides who sees them; the heartbeat interval is the machine policy's.

-- name: ListProcesses :many
SELECT pr.id, pr.account_id, pr.machine_id, pr.pid, pr.metadata, pr.last_heartbeat_at,
  pr.last_death_event_sent_at, pr.created_at, pr.updated_at,
  p.heartbeat_duration, p.heartbeat_resurrection_strategy, p.heartbeat_cull_strategy,
  l.product_id, m.license_id, count(*) OVER ()::bigint AS total
FROM lic.machine_processes pr
JOIN lic.machines m ON m.id = pr.machine_id
JOIN lic.licenses l ON l.id = m.license_id
JOIN lic.policies p ON p.id = m.policy_id
WHERE pr.account_id = @account_id
  AND (sqlc.narg(process_id)::uuid IS NULL OR pr.id = sqlc.narg(process_id))
  AND (sqlc.narg(filter_machine_id)::uuid IS NULL OR pr.machine_id = sqlc.narg(filter_machine_id))
  AND (sqlc.narg(filter_product_id)::uuid IS NULL OR l.product_id = sqlc.narg(filter_product_id))
  AND (sqlc.narg(filter_license_id)::uuid IS NULL OR m.license_id = sqlc.narg(filter_license_id))
  AND (sqlc.narg(filter_owner_id)::uuid IS NULL OR m.owner_id = sqlc.narg(filter_owner_id))
  AND (sqlc.narg(filter_user)::text IS NULL OR EXISTS (
    SELECT 1 FROM lic.users u
    WHERE u.account_id = pr.account_id AND (u.id::text = sqlc.narg(filter_user) OR u.email = sqlc.narg(filter_user))
      AND (l.user_id = u.id OR EXISTS (
        SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = u.id
      ))
  ))
  AND (sqlc.narg(filter_status)::text IS NULL OR CASE sqlc.narg(filter_status)::text
    WHEN 'ALIVE' THEN pr.last_heartbeat_at >= @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600))
    WHEN 'DEAD' THEN pr.last_heartbeat_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600))
    ELSE false
  END)
  AND (sqlc.narg(scope_kind)::text IS NULL
    OR (sqlc.narg(scope_kind)::text = 'product' AND l.product_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'license' AND m.license_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'user' AND (l.user_id = sqlc.narg(scope_id) OR EXISTS (
      SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = sqlc.narg(scope_id)
    ))))
  AND (sqlc.narg(cursor)::uuid IS NULL
    OR (@asc::boolean AND (pr.created_at, pr.id) > (SELECT x.created_at, x.id FROM lic.machine_processes x WHERE x.id = sqlc.narg(cursor)))
    OR (NOT @asc::boolean AND (pr.created_at, pr.id) < (SELECT x.created_at, x.id FROM lic.machine_processes x WHERE x.id = sqlc.narg(cursor))))
ORDER BY
  CASE WHEN @asc::boolean THEN pr.created_at END ASC,
  CASE WHEN @asc::boolean THEN pr.id END ASC,
  CASE WHEN NOT @asc::boolean THEN pr.created_at END DESC,
  CASE WHEN NOT @asc::boolean THEN pr.id END DESC
LIMIT @fetch OFFSET @page_offset;

-- The dead processes the cull visits (CullDeadProcessesWorker).
-- name: DeadProcesses :many
SELECT pr.id, pr.account_id
FROM lic.machine_processes pr
JOIN lic.machines m ON m.id = pr.machine_id
JOIN lic.policies p ON p.id = m.policy_id
WHERE p.heartbeat_cull_strategy <> 'ALWAYS_REVIVE'
  AND pr.last_heartbeat_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600))
ORDER BY pr.last_heartbeat_at, pr.id
LIMIT @batch;

-- name: InsertMachineProcess :exec
INSERT INTO lic.machine_processes (id, account_id, machine_id, pid, metadata, last_heartbeat_at, created_at, updated_at)
VALUES (@id, @account_id, @machine_id, @pid, @metadata, @last_heartbeat_at, @created_at, @updated_at);

-- name: UpdateMachineProcess :exec
UPDATE lic.machine_processes SET metadata = @metadata, last_heartbeat_at = @last_heartbeat_at,
  last_death_event_sent_at = @last_death_event_sent_at, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: DeleteMachineProcess :exec
DELETE FROM lic.machine_processes WHERE account_id = @account_id AND id = @id;

-- name: ProcessExists :one
SELECT EXISTS (SELECT 1 FROM lic.machine_processes WHERE id = @id);

-- name: ProcessPidTaken :one
SELECT EXISTS (SELECT 1 FROM lic.machine_processes WHERE machine_id = @machine_id AND pid = @pid);

-- The process counts the limits compare against (every process counts,
-- alive or dead): of a machine, of a license, of a license's owner.
-- name: ProcessCount :one
SELECT count(*)::bigint
FROM lic.machine_processes pr
JOIN lic.machines m ON m.id = pr.machine_id
WHERE CASE @kind::text
    WHEN 'machine' THEN pr.machine_id = @scope_id
    WHEN 'license' THEN m.license_id = @scope_id
    ELSE m.license_id = @scope_id AND m.owner_id IS NOT DISTINCT FROM sqlc.narg(owner_id)::uuid
  END;
