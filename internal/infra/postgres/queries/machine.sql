-- Machines (internal/app/machines): the row joined with the facts its
-- rules need — the policy's product and heartbeat rules, the license's
-- owner and resolved protected flag.

-- name: ListMachines :many
SELECT m.id, m.account_id, m.license_id, m.policy_id, m.owner_id, m.group_id,
  m.name, m.fingerprint, m.ip, m.hostname, m.platform, m.cores, m.memory, m.disk,
  m.max_processes_override, m.metadata, m.last_heartbeat_at, m.last_check_out_at,
  m.last_death_event_sent_at, m.created_at, m.updated_at,
  p.product_id, l.user_id AS license_owner_id,
  COALESCE(l.protected, p.protected, false)::boolean AS license_protected,
  p.require_heartbeat, p.heartbeat_duration, p.heartbeat_basis,
  p.heartbeat_resurrection_strategy, p.heartbeat_cull_strategy,
  COALESCE(NULLIF(l.max_processes_override, 0), p.max_processes) AS license_max_processes,
  count(*) OVER ()::bigint AS total
FROM lic.machines m
JOIN lic.licenses l ON l.id = m.license_id
JOIN lic.policies p ON p.id = m.policy_id
WHERE m.account_id = @account_id
  AND (sqlc.narg(filter_ids)::uuid[] IS NULL OR m.id = ANY(sqlc.narg(filter_ids)::uuid[]))
  AND (sqlc.narg(id_or_fingerprint)::text IS NULL
    OR m.id::text = sqlc.narg(id_or_fingerprint) OR m.fingerprint = sqlc.narg(id_or_fingerprint))
  AND (sqlc.narg(filter_fingerprint)::text IS NULL OR m.fingerprint = sqlc.narg(filter_fingerprint))
  AND (sqlc.narg(filter_ip)::text IS NULL OR m.ip = sqlc.narg(filter_ip))
  AND (sqlc.narg(filter_hostname)::text IS NULL OR m.hostname = sqlc.narg(filter_hostname))
  AND (sqlc.narg(filter_product_id)::uuid IS NULL OR l.product_id = sqlc.narg(filter_product_id))
  AND (sqlc.narg(filter_policy_id)::uuid IS NULL OR l.policy_id = sqlc.narg(filter_policy_id))
  AND (sqlc.narg(filter_license_id)::uuid IS NULL OR m.license_id = sqlc.narg(filter_license_id))
  AND (sqlc.narg(filter_key)::text IS NULL OR l.key = sqlc.narg(filter_key))
  AND (sqlc.narg(filter_group_id)::uuid IS NULL OR m.group_id = sqlc.narg(filter_group_id))
  AND (NOT @filter_owner_null::boolean OR m.owner_id IS NULL)
  AND (sqlc.narg(filter_owner)::text IS NULL OR m.owner_id IN (
    SELECT u.id FROM lic.users u
    WHERE u.account_id = m.account_id AND (u.id::text = sqlc.narg(filter_owner) OR u.email = sqlc.narg(filter_owner))
  ))
  AND (sqlc.narg(filter_user)::text IS NULL OR EXISTS (
    SELECT 1 FROM lic.users u
    WHERE u.account_id = m.account_id AND (u.id::text = sqlc.narg(filter_user) OR u.email = sqlc.narg(filter_user))
      AND (l.user_id = u.id OR EXISTS (
        SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = u.id
      ))
  ))
  AND (sqlc.narg(filter_metadata)::jsonb IS NULL
    OR m.metadata @> sqlc.narg(filter_metadata) OR m.metadata @> sqlc.narg(filter_metadata_cast))
  AND (sqlc.narg(filter_status)::text IS NULL OR CASE sqlc.narg(filter_status)::text
    WHEN 'ALIVE' THEN
      (m.last_heartbeat_at IS NULL AND NOT p.require_heartbeat)
      OR (m.last_heartbeat_at IS NULL AND p.require_heartbeat
        AND m.created_at >= @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
      OR (m.last_heartbeat_at IS NOT NULL
        AND m.last_heartbeat_at >= @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
    WHEN 'DEAD' THEN
      (m.last_heartbeat_at IS NULL AND p.require_heartbeat
        AND m.created_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
      OR (m.last_heartbeat_at IS NOT NULL
        AND m.last_heartbeat_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
    ELSE false
  END)
  AND (sqlc.narg(scope_kind)::text IS NULL
    OR (sqlc.narg(scope_kind)::text = 'product' AND l.product_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'license' AND m.license_id = sqlc.narg(scope_id))
    OR (sqlc.narg(scope_kind)::text = 'user' AND (l.user_id = sqlc.narg(scope_id) OR EXISTS (
      SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = sqlc.narg(scope_id)
    ))))
  AND (sqlc.narg(cursor)::uuid IS NULL
    OR (@asc::boolean AND (m.created_at, m.id) > (SELECT c.created_at, c.id FROM lic.machines c WHERE c.id = sqlc.narg(cursor)))
    OR (NOT @asc::boolean AND (m.created_at, m.id) < (SELECT c.created_at, c.id FROM lic.machines c WHERE c.id = sqlc.narg(cursor))))
ORDER BY
  CASE WHEN @asc::boolean THEN m.created_at END ASC,
  CASE WHEN @asc::boolean THEN m.id END ASC,
  CASE WHEN NOT @asc::boolean THEN m.created_at END DESC,
  CASE WHEN NOT @asc::boolean THEN m.id END DESC
LIMIT @fetch OFFSET @page_offset;

-- The dead machines the cull visits (CullDeadMachinesWorker): the dead
-- scope, the reference's cull-strategy condition verbatim (a NULL strategy
-- fails it), every account, a bounded batch.
-- name: DeadMachines :many
SELECT m.id, m.account_id
FROM lic.machines m
JOIN lic.policies p ON p.id = m.policy_id
WHERE p.heartbeat_cull_strategy <> 'ALWAYS_REVIVE'
  AND (
    (m.last_heartbeat_at IS NULL AND p.require_heartbeat
      AND m.created_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
    OR (m.last_heartbeat_at IS NOT NULL
      AND m.last_heartbeat_at < @now::timestamptz - make_interval(secs => COALESCE(p.heartbeat_duration, 600)))
  )
ORDER BY m.last_heartbeat_at NULLS FIRST, m.id
LIMIT @batch;

-- name: InsertMachine :exec
INSERT INTO lic.machines (
  id, account_id, license_id, policy_id, owner_id, group_id, name, fingerprint, ip, hostname, platform,
  cores, memory, disk, max_processes_override, metadata, last_heartbeat_at, created_at, updated_at
) VALUES (
  @id, @account_id, @license_id, @policy_id, @owner_id, @group_id, @name, @fingerprint, @ip, @hostname, @platform,
  @cores, @memory, @disk, @max_processes_override, @metadata, @last_heartbeat_at, @created_at, @updated_at
);

-- name: UpdateMachine :exec
UPDATE lic.machines SET
  owner_id = @owner_id, group_id = @group_id, name = @name, ip = @ip, hostname = @hostname,
  platform = @platform, cores = @cores, memory = @memory, disk = @disk,
  max_processes_override = @max_processes_override, metadata = @metadata,
  last_heartbeat_at = @last_heartbeat_at, last_check_out_at = @last_check_out_at,
  last_death_event_sent_at = @last_death_event_sent_at, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: DeleteMachine :exec
DELETE FROM lic.machines WHERE account_id = @account_id AND id = @id;

-- name: MachineExists :one
SELECT EXISTS (SELECT 1 FROM lic.machines WHERE account_id = @account_id AND id = @id);

-- name: FingerprintTaken :one
SELECT EXISTS (
  SELECT 1 FROM lic.machines m
  JOIN lic.licenses l ON l.id = m.license_id
  WHERE m.account_id = @account_id AND m.fingerprint = @fingerprint
    AND CASE @kind::text
      WHEN 'account' THEN true
      WHEN 'product' THEN l.product_id = @scope_id
      WHEN 'policy' THEN m.policy_id = @scope_id
      ELSE m.license_id = @scope_id
    END
);

-- name: MachineTotals :one
SELECT count(*)::bigint AS count,
  COALESCE(sum(cores), 0)::bigint AS cores,
  COALESCE(sum(memory), 0)::bigint AS memory,
  COALESCE(sum(disk), 0)::bigint AS disk
FROM lic.machines
WHERE license_id = @license_id AND id <> @except
  AND (NOT @per_user::boolean OR owner_id IS NOT DISTINCT FROM sqlc.narg(owner_id)::uuid);

-- name: RefreshLicenseMachineCounters :exec
UPDATE lic.licenses l SET
  machines_count = t.count, machines_core_count = t.cores,
  machines_memory_count = t.memory, machines_disk_count = t.disk, updated_at = @updated_at
FROM (
  SELECT count(*)::integer AS count, COALESCE(sum(cores), 0)::integer AS cores,
    COALESCE(sum(memory), 0)::bigint AS memory, COALESCE(sum(disk), 0)::bigint AS disk
  FROM lic.machines WHERE license_id = @license_id
) t
WHERE l.account_id = @account_id AND l.id = @license_id;

-- name: LockLicense :exec
SELECT 1 FROM lic.licenses WHERE account_id = @account_id AND id = @id FOR UPDATE;

-- name: LicenseHasUser :one
SELECT EXISTS (
  SELECT 1 FROM lic.licenses l WHERE l.id = @license_id AND l.user_id = @user_id
  UNION ALL
  SELECT 1 FROM lic.license_users lu WHERE lu.license_id = @license_id AND lu.user_id = @user_id
);

-- name: GroupMachineCount :one
SELECT count(*)::bigint FROM lic.machines WHERE group_id = @group_id;

-- name: InsertMachineComponent :exec
INSERT INTO lic.machine_components (id, account_id, machine_id, name, fingerprint, metadata, created_at, updated_at)
VALUES (@id, @account_id, @machine_id, @name, @fingerprint, @metadata, @created_at, @updated_at);

-- name: TokenForUpdateNowait :one
SELECT id, max_activations, activations, max_deactivations, deactivations
FROM lic.tokens WHERE account_id = @account_id AND id = @id
FOR UPDATE NOWAIT;

-- name: TokenForUpdateSkipLocked :one
SELECT id, max_activations, activations, max_deactivations, deactivations
FROM lic.tokens WHERE account_id = @account_id AND id = @id
FOR UPDATE SKIP LOCKED;

-- name: SetTokenActivationCounters :exec
UPDATE lic.tokens SET activations = @activations, deactivations = @deactivations, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;
