-- License authentication lookups (internal/app/authn). The license-auth
-- slice: the row, the owner's ban state and the policy's strategies.

-- name: GetLicenseAuthByKey :one
SELECT l.id, l.account_id, l.key, l.suspended, l.expiry, l.group_id,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  p.authentication_strategy, p.expiration_strategy
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id AND l.key = @key;

-- name: GetLicenseAuthByID :one
SELECT l.id, l.account_id, l.key, l.suspended, l.expiry, l.group_id,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  p.authentication_strategy, p.expiration_strategy
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id AND l.id = @id;

-- Licenses (internal/app/licenses): the full resource — row plus its
-- policy and owner context, CRUD, filters, relationships and actions.


-- name: GetLicenseByID :one
SELECT l.id, l.account_id, l.product_id, l.policy_id, l.user_id, l.group_id,
  l.name, l.key, l.expiry, l.uses, l.suspended, l.protected, l.metadata,
  l.max_machines_override, l.max_processes_override, l.max_users_override,
  l.max_cores_override, l.max_memory_override, l.max_disk_override,
  l.max_uses_override, l.machines_count, l.machines_core_count,
  l.machines_memory_count, l.machines_disk_count, l.license_users_count,
  l.last_validated_at, l.last_validated_checksum, l.last_validated_version,
  l.last_check_in_at, l.last_check_out_at, l.created_at, l.updated_at,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  u.email AS owner_email,
  p.id AS policy_id2, p.account_id AS policy_account_id, p.product_id AS policy_product_id,
  p.name AS policy_name, p.duration AS policy_duration, p.strict AS policy_strict,
  p.floating AS policy_floating, p.use_pool AS policy_use_pool,
  p.max_machines AS policy_max_machines, p.max_processes AS policy_max_processes,
  p.max_users AS policy_max_users, p.max_cores AS policy_max_cores,
  p.max_memory AS policy_max_memory, p.max_disk AS policy_max_disk,
  p.max_uses AS policy_max_uses,
  p.machine_uniqueness_strategy, p.machine_matching_strategy,
  p.component_uniqueness_strategy, p.component_matching_strategy,
  p.expiration_strategy, p.expiration_basis, p.renewal_basis, p.transfer_strategy,
  p.authentication_strategy, p.machine_leasing_strategy, p.process_leasing_strategy,
  p.overage_strategy, p.scheme AS policy_scheme, p.encrypted AS policy_encrypted,
  p.protected AS policy_protected,
  p.require_product_scope, p.require_policy_scope, p.require_machine_scope,
  p.require_fingerprint_scope, p.require_components_scope, p.require_user_scope,
  p.require_checksum_scope, p.require_version_scope, p.require_check_in,
  p.check_in_interval, p.check_in_interval_count, p.heartbeat_duration,
  p.heartbeat_cull_strategy, p.heartbeat_resurrection_strategy, p.heartbeat_basis,
  p.require_heartbeat, p.metadata AS policy_metadata,
  p.created_at AS policy_created_at, p.updated_at AS policy_updated_at
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id AND l.id = @id;

-- name: GetLicenseByKey :one
SELECT l.id, l.account_id, l.product_id, l.policy_id, l.user_id, l.group_id,
  l.name, l.key, l.expiry, l.uses, l.suspended, l.protected, l.metadata,
  l.max_machines_override, l.max_processes_override, l.max_users_override,
  l.max_cores_override, l.max_memory_override, l.max_disk_override,
  l.max_uses_override, l.machines_count, l.machines_core_count,
  l.machines_memory_count, l.machines_disk_count, l.license_users_count,
  l.last_validated_at, l.last_validated_checksum, l.last_validated_version,
  l.last_check_in_at, l.last_check_out_at, l.created_at, l.updated_at,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  u.email AS owner_email,
  p.id AS policy_id2, p.account_id AS policy_account_id, p.product_id AS policy_product_id,
  p.name AS policy_name, p.duration AS policy_duration, p.strict AS policy_strict,
  p.floating AS policy_floating, p.use_pool AS policy_use_pool,
  p.max_machines AS policy_max_machines, p.max_processes AS policy_max_processes,
  p.max_users AS policy_max_users, p.max_cores AS policy_max_cores,
  p.max_memory AS policy_max_memory, p.max_disk AS policy_max_disk,
  p.max_uses AS policy_max_uses,
  p.machine_uniqueness_strategy, p.machine_matching_strategy,
  p.component_uniqueness_strategy, p.component_matching_strategy,
  p.expiration_strategy, p.expiration_basis, p.renewal_basis, p.transfer_strategy,
  p.authentication_strategy, p.machine_leasing_strategy, p.process_leasing_strategy,
  p.overage_strategy, p.scheme AS policy_scheme, p.encrypted AS policy_encrypted,
  p.protected AS policy_protected,
  p.require_product_scope, p.require_policy_scope, p.require_machine_scope,
  p.require_fingerprint_scope, p.require_components_scope, p.require_user_scope,
  p.require_checksum_scope, p.require_version_scope, p.require_check_in,
  p.check_in_interval, p.check_in_interval_count, p.heartbeat_duration,
  p.heartbeat_cull_strategy, p.heartbeat_resurrection_strategy, p.heartbeat_basis,
  p.require_heartbeat, p.metadata AS policy_metadata,
  p.created_at AS policy_created_at, p.updated_at AS policy_updated_at
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id AND l.key = @key;

-- name: InsertLicense :exec
INSERT INTO lic.licenses (
  id, account_id, product_id, policy_id, user_id, group_id, name, key, expiry,
  uses, suspended, protected, metadata, max_machines_override,
  max_processes_override, max_users_override, max_cores_override,
  max_memory_override, max_disk_override, max_uses_override, last_check_in_at
) VALUES (
  @id, @account_id, @product_id, @policy_id, @user_id, @group_id, @name, @key,
  @expiry, @uses, @suspended, @protected, @metadata, @max_machines_override,
  @max_processes_override, @max_users_override, @max_cores_override,
  @max_memory_override, @max_disk_override, @max_uses_override, @last_check_in_at
);

-- name: UpdateLicense :exec
UPDATE lic.licenses SET
  name = @name,
  expiry = @expiry,
  uses = @uses,
  suspended = @suspended,
  protected = @protected,
  metadata = @metadata,
  max_machines_override = @max_machines_override,
  max_processes_override = @max_processes_override,
  max_users_override = @max_users_override,
  max_cores_override = @max_cores_override,
  max_memory_override = @max_memory_override,
  max_disk_override = @max_disk_override,
  max_uses_override = @max_uses_override,
  updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: DeleteLicense :execrows
DELETE FROM lic.licenses WHERE account_id = @account_id AND id = @id;

-- name: DeleteTokensOfLicense :exec
DELETE FROM lic.tokens WHERE account_id = @account_id
  AND bearer_type = 'License' AND bearer_id = @bearer_id;

-- name: LicenseKeyTaken :one
SELECT id FROM lic.licenses WHERE account_id = @account_id AND key = @key;

-- name: LicenseIDExists :one
SELECT id FROM lic.licenses WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseExpiry :exec
UPDATE lic.licenses SET expiry = @expiry, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseUses :exec
UPDATE lic.licenses SET uses = @uses, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseSuspended :exec
UPDATE lic.licenses SET suspended = @suspended, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: TouchLicenseValidated :exec
UPDATE lic.licenses SET
  last_validated_at = @last_validated_at,
  last_validated_checksum = COALESCE(@last_validated_checksum, last_validated_checksum),
  last_validated_version = COALESCE(@last_validated_version, last_validated_version),
  updated_at = @last_validated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseLastCheckIn :exec
UPDATE lic.licenses SET last_check_in_at = @last_check_in_at, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseLastCheckOut :exec
UPDATE lic.licenses SET last_check_out_at = @last_check_out_at, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: TransferLicense :exec
UPDATE lic.licenses SET
  policy_id = @policy_id,
  product_id = @product_id,
  expiry = @expiry,
  updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseOwner :exec
UPDATE lic.licenses SET user_id = @user_id, group_id = @group_id, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: SetLicenseGroup :exec
UPDATE lic.licenses SET group_id = @group_id, updated_at = @updated_at
WHERE account_id = @account_id AND id = @id;

-- name: CountLicensesOfGroup :one
SELECT count(*) AS count FROM lic.licenses
WHERE account_id = @account_id AND group_id = @group_id;

-- name: InsertLicenseUsers :exec
INSERT INTO lic.license_users (id, account_id, license_id, user_id)
SELECT @id, @account_id, @license_id, x AS user_id FROM unnest(@user_ids::uuid[]) AS x;

-- name: DeleteLicenseUsers :execrows
DELETE FROM lic.license_users
WHERE account_id = @account_id AND license_id = @license_id AND user_id = ANY(@user_ids::uuid[]);

-- name: CountLicenseUsersOf :one
SELECT count(*) AS count FROM lic.license_users
WHERE account_id = @account_id AND license_id = @license_id AND user_id = ANY(@user_ids::uuid[]);

-- name: SetLicenseUsersCount :exec
UPDATE lic.licenses SET license_users_count = @count
WHERE account_id = @account_id AND id = @id;

-- name: ListLicenseUserRows :many
SELECT lu.id, lu.account_id, lu.license_id, lu.user_id, lu.created_at, lu.updated_at
FROM lic.license_users lu
WHERE lu.account_id = @account_id AND lu.license_id = @license_id
  AND lu.user_id = ANY(@user_ids::uuid[]);

-- name: LicenseeIsUser :one
SELECT 1 AS found FROM lic.license_users
WHERE account_id = @account_id AND license_id = @license_id AND user_id = @user_id;

-- name: InsertLicenseEntitlements :exec
INSERT INTO lic.license_entitlements (id, account_id, license_id, entitlement_id)
SELECT @id, @account_id, @license_id, x AS entitlement_id FROM unnest(@entitlement_ids::uuid[]) AS x;

-- name: DeleteLicenseEntitlements :execrows
DELETE FROM lic.license_entitlements
WHERE account_id = @account_id AND license_id = @license_id
  AND entitlement_id = ANY(@entitlement_ids::uuid[]);

-- name: CountLicenseEntitlementsOf :one
SELECT count(*) AS count FROM lic.license_entitlements
WHERE account_id = @account_id AND license_id = @license_id
  AND entitlement_id = ANY(@entitlement_ids::uuid[]);

-- name: ListLicenseEntitlementRows :many
SELECT le.id, le.account_id, le.license_id, le.entitlement_id, le.created_at, le.updated_at
FROM lic.license_entitlements le
WHERE le.account_id = @account_id AND le.license_id = @license_id
  AND le.entitlement_id = ANY(@entitlement_ids::uuid[]);

-- name: CountPolicyEntitlementsOf :one
SELECT count(*) AS count FROM lic.policy_entitlements
WHERE account_id = @account_id AND policy_id = @policy_id
  AND entitlement_id = ANY(@entitlement_ids::uuid[]);

-- name: ListLicenseEntitlementCodes :many
SELECT DISTINCT e.code
FROM lic.entitlements e
LEFT JOIN lic.policy_entitlements pe ON pe.entitlement_id = e.id AND pe.policy_id = @policy_id
LEFT JOIN lic.license_entitlements le ON le.entitlement_id = e.id AND le.license_id = @license_id
WHERE e.account_id = @account_id AND (pe.id IS NOT NULL OR le.id IS NOT NULL)
ORDER BY e.code;

-- License-scope validation helpers: the machines and processes behind
-- a license, with the heartbeat facts the alive/dead math needs.

-- name: GetLicenseMachineByID :one
SELECT m.id, m.account_id, m.license_id, m.policy_id, m.owner_id, m.group_id,
  m.name, m.fingerprint, m.ip, m.hostname, m.platform, m.cores, m.memory, m.disk,
  m.max_processes_override, m.metadata, m.last_heartbeat_at, m.last_check_out_at,
  m.created_at, m.updated_at,
  p.require_heartbeat, p.heartbeat_duration
FROM lic.machines m
JOIN lic.policies p ON p.id = m.policy_id
WHERE m.account_id = @account_id AND m.id = @id AND m.license_id = @license_id;

-- name: GetLicenseMachineByFingerprint :one
SELECT m.id, m.account_id, m.license_id, m.policy_id, m.owner_id, m.group_id,
  m.name, m.fingerprint, m.ip, m.hostname, m.platform, m.cores, m.memory, m.disk,
  m.max_processes_override, m.metadata, m.last_heartbeat_at, m.last_check_out_at,
  m.created_at, m.updated_at,
  p.require_heartbeat, p.heartbeat_duration
FROM lic.machines m
JOIN lic.policies p ON p.id = m.policy_id
WHERE m.account_id = @account_id AND m.fingerprint = @fingerprint AND m.license_id = @license_id;

-- name: CountLicenseProcesses :one
SELECT count(*) AS count FROM lic.machine_processes mp
JOIN lic.machines m ON m.id = mp.machine_id
WHERE m.account_id = @account_id AND m.license_id = @license_id
  AND (@owner_id::uuid IS NULL OR m.owner_id = @owner_id);

-- name: CountMachineProcesses :one
SELECT count(*) AS count FROM lic.machine_processes mp
WHERE mp.account_id = @account_id AND mp.machine_id = @machine_id;

-- The machine rows of a license (relationship index and validation).
-- heartbeat_status mirrors Machine#heartbeat_status with the default
-- TTL of 10 minutes.
-- name: ListLicenseMachines :many
SELECT m.id, m.account_id, m.license_id, m.policy_id, m.owner_id, m.group_id,
  m.name, m.fingerprint, m.ip, m.hostname, m.platform, m.cores, m.memory, m.disk,
  m.max_processes_override, m.metadata, m.last_heartbeat_at, m.last_check_out_at,
  m.created_at, m.updated_at,
  p.require_heartbeat, p.heartbeat_duration,
  CASE
    WHEN m.last_heartbeat_at IS NULL
      AND (p.require_heartbeat = false
        OR m.created_at >= (@now::timestamptz - INTERVAL '1 second' * COALESCE(p.heartbeat_duration, @heartbeat_ttl)))
      THEN 'NOT_STARTED'
    WHEN m.last_heartbeat_at IS NOT NULL
      AND m.last_heartbeat_at >= (@now::timestamptz - INTERVAL '1 second' * COALESCE(p.heartbeat_duration, @heartbeat_ttl))
      THEN 'ALIVE'
    ELSE 'DEAD'
  END AS heartbeat_status
FROM lic.machines m
JOIN lic.policies p ON p.id = m.policy_id
WHERE m.account_id = @account_id
  AND m.license_id = @license_id
  AND (@fingerprint::text IS NULL OR m.fingerprint = @fingerprint)
  AND (@product_id::uuid IS NULL OR m.license_id IN (
    SELECT l2.id FROM lic.licenses l2 WHERE l2.product_id = @product_id
  ))
  AND (
    @user_id::uuid IS NULL OR EXISTS (
      SELECT 1 FROM lic.licenses l3
      LEFT JOIN lic.license_users lu ON lu.license_id = l3.id
      WHERE l3.id = m.license_id AND (l3.user_id = @user_id OR lu.user_id = @user_id)
    )
  )
ORDER BY m.created_at DESC, m.id DESC
LIMIT @limit;

-- name: CountLicenseMachines :one
SELECT count(*) AS count FROM lic.machines
WHERE account_id = @account_id AND license_id = @license_id;

-- The license index: every has_scope filter of the reference's
-- controller, the bearer scope and the pagination window. The
-- count shares the exact WHERE.
-- name: ListLicenses :many
SELECT l.id, l.account_id, l.product_id, l.policy_id, l.user_id, l.group_id,
  l.name, l.key, l.expiry, l.uses, l.suspended, l.protected, l.metadata,
  l.max_machines_override, l.max_processes_override, l.max_users_override,
  l.max_cores_override, l.max_memory_override, l.max_disk_override,
  l.max_uses_override, l.machines_count, l.machines_core_count,
  l.machines_memory_count, l.machines_disk_count, l.license_users_count,
  l.last_validated_at, l.last_validated_checksum, l.last_validated_version,
  l.last_check_in_at, l.last_check_out_at, l.created_at, l.updated_at,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  u.email AS owner_email,
  p.id AS policy_id2, p.account_id AS policy_account_id, p.product_id AS policy_product_id,
  p.name AS policy_name, p.duration AS policy_duration, p.strict AS policy_strict,
  p.floating AS policy_floating, p.use_pool AS policy_use_pool,
  p.max_machines AS policy_max_machines, p.max_processes AS policy_max_processes,
  p.max_users AS policy_max_users, p.max_cores AS policy_max_cores,
  p.max_memory AS policy_max_memory, p.max_disk AS policy_max_disk,
  p.max_uses AS policy_max_uses,
  p.machine_uniqueness_strategy, p.machine_matching_strategy,
  p.component_uniqueness_strategy, p.component_matching_strategy,
  p.expiration_strategy, p.expiration_basis, p.renewal_basis, p.transfer_strategy,
  p.authentication_strategy, p.machine_leasing_strategy, p.process_leasing_strategy,
  p.overage_strategy, p.scheme AS policy_scheme, p.encrypted AS policy_encrypted,
  p.protected AS policy_protected,
  p.require_product_scope, p.require_policy_scope, p.require_machine_scope,
  p.require_fingerprint_scope, p.require_components_scope, p.require_user_scope,
  p.require_checksum_scope, p.require_version_scope, p.require_check_in,
  p.check_in_interval, p.check_in_interval_count, p.heartbeat_duration,
  p.heartbeat_cull_strategy, p.heartbeat_resurrection_strategy, p.heartbeat_basis,
  p.require_heartbeat, p.metadata AS policy_metadata,
  p.created_at AS policy_created_at, p.updated_at AS policy_updated_at
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR l.product_id = @filter_product_id)
  AND (@filter_policy_id::uuid IS NULL OR l.policy_id = @filter_policy_id)
  AND (@filter_owner_id::uuid IS NULL OR l.user_id = @filter_owner_id)
  AND (@filter_owner_email::text IS NULL OR u.email = @filter_owner_email)
  AND (@filter_owner_email_like::text IS NULL OR u.email ILIKE '%' || @filter_owner_email_like || '%')
  AND (
    @filter_user_id::uuid IS NULL OR l.user_id = @filter_user_id OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = l.id AND lu.user_id = @filter_user_id
    )
  )
  AND (
    @filter_user_email::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users WHERE account_id = l.account_id AND email = @filter_user_email
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email = @filter_user_email
    )
  )
  AND (
    @filter_user_email_like::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users
      WHERE account_id = l.account_id AND email ILIKE '%' || @filter_user_email_like || '%'
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email ILIKE '%' || @filter_user_email_like || '%'
    )
  )
  AND (
    @filter_machine_id::uuid IS NULL OR EXISTS (
      SELECT 1 FROM lic.machines m WHERE m.license_id = l.id AND m.id = @filter_machine_id
    )
  )
  AND (@filter_group_id::uuid IS NULL OR l.group_id = @filter_group_id)
  AND (
    @filter_status::text IS NULL
    OR CASE
      WHEN l.user_id IS NOT NULL AND u.banned_at IS NOT NULL THEN 'BANNED'
      WHEN l.suspended THEN 'SUSPENDED'
      WHEN l.expiry IS NOT NULL AND l.expiry < @now THEN 'EXPIRED'
      WHEN l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon THEN 'EXPIRING'
      WHEN NOT (
        l.created_at >= @activity_cutoff
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_cutoff)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_cutoff)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_cutoff)
      ) THEN 'INACTIVE'
      ELSE 'ACTIVE'
    END = @filter_status
  )
  AND (@filter_suspended::boolean IS NULL OR l.suspended = @filter_suspended)
  AND (
    @filter_expiring::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon) = @filter_expiring
  )
  AND (
    @filter_expired::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry < @now) = @filter_expired
  )
  AND (
    @filter_unexpired::boolean IS NULL
    OR (l.expiry IS NULL OR l.expiry >= @now) = @filter_unexpired
  )
  AND (
    @expiry_gte1::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte1)
  )
  AND (
    @expiry_gte2::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte2)
  )
  AND (
    @expiry_lte::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry <= @expiry_lte)
  )
  AND (
    @activity_active_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND (
        l.created_at >= @activity_active_since
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_active_since)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_active_since)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_active_since)
      )
    )
  )
  AND (
    @activity_inactive_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND l.created_at < @activity_inactive_since
      AND (l.last_validated_at IS NULL OR l.last_validated_at < @activity_inactive_since)
      AND (l.last_check_out_at IS NULL OR l.last_check_out_at < @activity_inactive_since)
      AND (l.last_check_in_at IS NULL OR l.last_check_in_at < @activity_inactive_since)
    )
  )
  AND (
    @filter_metadata::jsonb IS NULL
    OR l.metadata @> @filter_metadata
    OR l.metadata @> @filter_metadata_cast
  )
  AND (
    @filter_assigned::boolean IS NULL
    OR (
      l.user_id IS NOT NULL
      OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_assigned
  )
  AND (
    @filter_unassigned::boolean IS NULL
    OR (
      l.user_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_unassigned
  )
  AND (
    @filter_activated::boolean IS NULL
    OR (l.machines_count >= 1) = @filter_activated
  )
  AND (@activations_eq::integer IS NULL OR l.machines_count = @activations_eq)
  AND (@activations_gt::integer IS NULL OR l.machines_count > @activations_gt)
  AND (@activations_gte::integer IS NULL OR l.machines_count >= @activations_gte)
  AND (@activations_lt::integer IS NULL OR l.machines_count < @activations_lt)
  AND (@activations_lte::integer IS NULL OR l.machines_count <= @activations_lte)
  AND (
    @scope_kind::text IS NULL
    OR (@scope_kind::text = 'none' AND false)
    OR (@scope_kind::text = 'product' AND l.product_id = @scope_id)
    OR (
      @scope_kind::text = 'user'
      AND (
        l.user_id = @scope_id
        OR EXISTS (
          SELECT 1 FROM lic.license_users lu
          WHERE lu.license_id = l.id AND lu.user_id = @scope_id
        )
      )
    )
    OR (@scope_kind::text = 'license' AND l.id = @scope_id)
  )
  AND (
    @cursor::uuid IS NULL
    OR (l.created_at, l.id) < (SELECT created_at, id FROM lic.licenses WHERE id = @cursor)
  )
ORDER BY l.created_at DESC, l.id DESC
LIMIT @limit OFFSET @offset;

-- name: ListLicensesAsc :many
SELECT l.id, l.account_id, l.product_id, l.policy_id, l.user_id, l.group_id,
  l.name, l.key, l.expiry, l.uses, l.suspended, l.protected, l.metadata,
  l.max_machines_override, l.max_processes_override, l.max_users_override,
  l.max_cores_override, l.max_memory_override, l.max_disk_override,
  l.max_uses_override, l.machines_count, l.machines_core_count,
  l.machines_memory_count, l.machines_disk_count, l.license_users_count,
  l.last_validated_at, l.last_validated_checksum, l.last_validated_version,
  l.last_check_in_at, l.last_check_out_at, l.created_at, l.updated_at,
  (l.user_id IS NOT NULL AND u.banned_at IS NOT NULL) AS owner_banned,
  u.email AS owner_email,
  p.id AS policy_id2, p.account_id AS policy_account_id, p.product_id AS policy_product_id,
  p.name AS policy_name, p.duration AS policy_duration, p.strict AS policy_strict,
  p.floating AS policy_floating, p.use_pool AS policy_use_pool,
  p.max_machines AS policy_max_machines, p.max_processes AS policy_max_processes,
  p.max_users AS policy_max_users, p.max_cores AS policy_max_cores,
  p.max_memory AS policy_max_memory, p.max_disk AS policy_max_disk,
  p.max_uses AS policy_max_uses,
  p.machine_uniqueness_strategy, p.machine_matching_strategy,
  p.component_uniqueness_strategy, p.component_matching_strategy,
  p.expiration_strategy, p.expiration_basis, p.renewal_basis, p.transfer_strategy,
  p.authentication_strategy, p.machine_leasing_strategy, p.process_leasing_strategy,
  p.overage_strategy, p.scheme AS policy_scheme, p.encrypted AS policy_encrypted,
  p.protected AS policy_protected,
  p.require_product_scope, p.require_policy_scope, p.require_machine_scope,
  p.require_fingerprint_scope, p.require_components_scope, p.require_user_scope,
  p.require_checksum_scope, p.require_version_scope, p.require_check_in,
  p.check_in_interval, p.check_in_interval_count, p.heartbeat_duration,
  p.heartbeat_cull_strategy, p.heartbeat_resurrection_strategy, p.heartbeat_basis,
  p.require_heartbeat, p.metadata AS policy_metadata,
  p.created_at AS policy_created_at, p.updated_at AS policy_updated_at
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR l.product_id = @filter_product_id)
  AND (@filter_policy_id::uuid IS NULL OR l.policy_id = @filter_policy_id)
  AND (@filter_owner_id::uuid IS NULL OR l.user_id = @filter_owner_id)
  AND (@filter_owner_email::text IS NULL OR u.email = @filter_owner_email)
  AND (@filter_owner_email_like::text IS NULL OR u.email ILIKE '%' || @filter_owner_email_like || '%')
  AND (
    @filter_user_id::uuid IS NULL OR l.user_id = @filter_user_id OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = l.id AND lu.user_id = @filter_user_id
    )
  )
  AND (
    @filter_user_email::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users WHERE account_id = l.account_id AND email = @filter_user_email
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email = @filter_user_email
    )
  )
  AND (
    @filter_user_email_like::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users
      WHERE account_id = l.account_id AND email ILIKE '%' || @filter_user_email_like || '%'
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email ILIKE '%' || @filter_user_email_like || '%'
    )
  )
  AND (
    @filter_machine_id::uuid IS NULL OR EXISTS (
      SELECT 1 FROM lic.machines m WHERE m.license_id = l.id AND m.id = @filter_machine_id
    )
  )
  AND (@filter_group_id::uuid IS NULL OR l.group_id = @filter_group_id)
  AND (
    @filter_status::text IS NULL
    OR CASE
      WHEN l.user_id IS NOT NULL AND u.banned_at IS NOT NULL THEN 'BANNED'
      WHEN l.suspended THEN 'SUSPENDED'
      WHEN l.expiry IS NOT NULL AND l.expiry < @now THEN 'EXPIRED'
      WHEN l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon THEN 'EXPIRING'
      WHEN NOT (
        l.created_at >= @activity_cutoff
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_cutoff)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_cutoff)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_cutoff)
      ) THEN 'INACTIVE'
      ELSE 'ACTIVE'
    END = @filter_status
  )
  AND (@filter_suspended::boolean IS NULL OR l.suspended = @filter_suspended)
  AND (
    @filter_expiring::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon) = @filter_expiring
  )
  AND (
    @filter_expired::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry < @now) = @filter_expired
  )
  AND (
    @filter_unexpired::boolean IS NULL
    OR (l.expiry IS NULL OR l.expiry >= @now) = @filter_unexpired
  )
  AND (
    @expiry_gte1::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte1)
  )
  AND (
    @expiry_gte2::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte2)
  )
  AND (
    @expiry_lte::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry <= @expiry_lte)
  )
  AND (
    @activity_active_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND (
        l.created_at >= @activity_active_since
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_active_since)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_active_since)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_active_since)
      )
    )
  )
  AND (
    @activity_inactive_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND l.created_at < @activity_inactive_since
      AND (l.last_validated_at IS NULL OR l.last_validated_at < @activity_inactive_since)
      AND (l.last_check_out_at IS NULL OR l.last_check_out_at < @activity_inactive_since)
      AND (l.last_check_in_at IS NULL OR l.last_check_in_at < @activity_inactive_since)
    )
  )
  AND (
    @filter_metadata::jsonb IS NULL
    OR l.metadata @> @filter_metadata
    OR l.metadata @> @filter_metadata_cast
  )
  AND (
    @filter_assigned::boolean IS NULL
    OR (
      l.user_id IS NOT NULL
      OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_assigned
  )
  AND (
    @filter_unassigned::boolean IS NULL
    OR (
      l.user_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_unassigned
  )
  AND (
    @filter_activated::boolean IS NULL
    OR (l.machines_count >= 1) = @filter_activated
  )
  AND (@activations_eq::integer IS NULL OR l.machines_count = @activations_eq)
  AND (@activations_gt::integer IS NULL OR l.machines_count > @activations_gt)
  AND (@activations_gte::integer IS NULL OR l.machines_count >= @activations_gte)
  AND (@activations_lt::integer IS NULL OR l.machines_count < @activations_lt)
  AND (@activations_lte::integer IS NULL OR l.machines_count <= @activations_lte)
  AND (
    @scope_kind::text IS NULL
    OR (@scope_kind::text = 'none' AND false)
    OR (@scope_kind::text = 'product' AND l.product_id = @scope_id)
    OR (
      @scope_kind::text = 'user'
      AND (
        l.user_id = @scope_id
        OR EXISTS (
          SELECT 1 FROM lic.license_users lu
          WHERE lu.license_id = l.id AND lu.user_id = @scope_id
        )
      )
    )
    OR (@scope_kind::text = 'license' AND l.id = @scope_id)
  )
  AND (
    @cursor::uuid IS NULL
    OR (l.created_at, l.id) > (SELECT created_at, id FROM lic.licenses WHERE id = @cursor)
  )
ORDER BY l.created_at ASC, l.id ASC
LIMIT @limit OFFSET @offset;

-- name: CountLicenses :one
SELECT count(*) AS count
FROM lic.licenses l
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR l.product_id = @filter_product_id)
  AND (@filter_policy_id::uuid IS NULL OR l.policy_id = @filter_policy_id)
  AND (@filter_owner_id::uuid IS NULL OR l.user_id = @filter_owner_id)
  AND (@filter_owner_email::text IS NULL OR u.email = @filter_owner_email)
  AND (@filter_owner_email_like::text IS NULL OR u.email ILIKE '%' || @filter_owner_email_like || '%')
  AND (
    @filter_user_id::uuid IS NULL OR l.user_id = @filter_user_id OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = l.id AND lu.user_id = @filter_user_id
    )
  )
  AND (
    @filter_user_email::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users WHERE account_id = l.account_id AND email = @filter_user_email
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email = @filter_user_email
    )
  )
  AND (
    @filter_user_email_like::text IS NULL
    OR l.user_id IN (
      SELECT id FROM lic.users
      WHERE account_id = l.account_id AND email ILIKE '%' || @filter_user_email_like || '%'
    )
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      JOIN lic.users luu ON luu.id = lu.user_id
      WHERE lu.license_id = l.id AND luu.email ILIKE '%' || @filter_user_email_like || '%'
    )
  )
  AND (
    @filter_machine_id::uuid IS NULL OR EXISTS (
      SELECT 1 FROM lic.machines m WHERE m.license_id = l.id AND m.id = @filter_machine_id
    )
  )
  AND (@filter_group_id::uuid IS NULL OR l.group_id = @filter_group_id)
  AND (
    @filter_status::text IS NULL
    OR CASE
      WHEN l.user_id IS NOT NULL AND u.banned_at IS NOT NULL THEN 'BANNED'
      WHEN l.suspended THEN 'SUSPENDED'
      WHEN l.expiry IS NOT NULL AND l.expiry < @now THEN 'EXPIRED'
      WHEN l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon THEN 'EXPIRING'
      WHEN NOT (
        l.created_at >= @activity_cutoff
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_cutoff)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_cutoff)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_cutoff)
      ) THEN 'INACTIVE'
      ELSE 'ACTIVE'
    END = @filter_status
  )
  AND (@filter_suspended::boolean IS NULL OR l.suspended = @filter_suspended)
  AND (
    @filter_expiring::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry > @now AND l.expiry < @expiring_soon) = @filter_expiring
  )
  AND (
    @filter_expired::boolean IS NULL
    OR (l.expiry IS NOT NULL AND l.expiry < @now) = @filter_expired
  )
  AND (
    @filter_unexpired::boolean IS NULL
    OR (l.expiry IS NULL OR l.expiry >= @now) = @filter_unexpired
  )
  AND (
    @expiry_gte1::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte1)
  )
  AND (
    @expiry_gte2::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry >= @expiry_gte2)
  )
  AND (
    @expiry_lte::timestamptz IS NULL OR (l.expiry IS NOT NULL AND l.expiry <= @expiry_lte)
  )
  AND (
    @activity_active_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND (
        l.created_at >= @activity_active_since
        OR (l.last_validated_at IS NOT NULL AND l.last_validated_at >= @activity_active_since)
        OR (l.last_check_out_at IS NOT NULL AND l.last_check_out_at >= @activity_active_since)
        OR (l.last_check_in_at IS NOT NULL AND l.last_check_in_at >= @activity_active_since)
      )
    )
  )
  AND (
    @activity_inactive_since::timestamptz IS NULL
    OR (
      (l.user_id IS NULL OR u.banned_at IS NULL)
      AND l.created_at < @activity_inactive_since
      AND (l.last_validated_at IS NULL OR l.last_validated_at < @activity_inactive_since)
      AND (l.last_check_out_at IS NULL OR l.last_check_out_at < @activity_inactive_since)
      AND (l.last_check_in_at IS NULL OR l.last_check_in_at < @activity_inactive_since)
    )
  )
  AND (
    @filter_metadata::jsonb IS NULL
    OR l.metadata @> @filter_metadata
    OR l.metadata @> @filter_metadata_cast
  )
  AND (
    @filter_assigned::boolean IS NULL
    OR (
      l.user_id IS NOT NULL
      OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_assigned
  )
  AND (
    @filter_unassigned::boolean IS NULL
    OR (
      l.user_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id)
    ) = @filter_unassigned
  )
  AND (
    @filter_activated::boolean IS NULL
    OR (l.machines_count >= 1) = @filter_activated
  )
  AND (@activations_eq::integer IS NULL OR l.machines_count = @activations_eq)
  AND (@activations_gt::integer IS NULL OR l.machines_count > @activations_gt)
  AND (@activations_gte::integer IS NULL OR l.machines_count >= @activations_gte)
  AND (@activations_lt::integer IS NULL OR l.machines_count < @activations_lt)
  AND (@activations_lte::integer IS NULL OR l.machines_count <= @activations_lte)
  AND (
    @scope_kind::text IS NULL
    OR (@scope_kind::text = 'none' AND false)
    OR (@scope_kind::text = 'product' AND l.product_id = @scope_id)
    OR (
      @scope_kind::text = 'user'
      AND (
        l.user_id = @scope_id
        OR EXISTS (
          SELECT 1 FROM lic.license_users lu
          WHERE lu.license_id = l.id AND lu.user_id = @scope_id
        )
      )
    )
    OR (@scope_kind::text = 'license' AND l.id = @scope_id)
  )
;

-- Validation helpers (LicenseValidationService).

-- name: LicenseUserMatches :one
SELECT 1 AS found FROM lic.users usr
WHERE usr.account_id = @account_id
  AND (usr.id = @user_id OR (@email::text IS NOT NULL AND usr.email = @email))
  AND EXISTS (
    SELECT 1 FROM lic.licenses l
    LEFT JOIN lic.license_users lu ON lu.license_id = l.id
    WHERE l.id = @license_id AND (l.user_id = usr.id OR lu.user_id = usr.id)
  );

-- name: SumLicenseMachinesForOwner :one
SELECT count(*) AS count,
  COALESCE(sum(m.cores), 0)::int AS cores,
  COALESCE(sum(m.memory), 0)::bigint AS memory,
  COALESCE(sum(m.disk), 0)::bigint AS disk
FROM lic.machines m
WHERE m.account_id = @account_id AND m.license_id = @license_id
  AND m.owner_id IS NOT DISTINCT FROM @owner_id;

-- name: CountMachineComponentsOf :one
SELECT count(DISTINCT mc.fingerprint) AS count
FROM lic.machine_components mc
WHERE mc.machine_id = @machine_id AND mc.fingerprint = ANY(@fingerprints::text[]);

-- name: CountLicenseEntitlementCodes :one
SELECT count(DISTINCT e.code) AS count
FROM lic.entitlements e
LEFT JOIN lic.policy_entitlements pe ON pe.entitlement_id = e.id AND pe.policy_id = @policy_id
LEFT JOIN lic.license_entitlements le ON le.entitlement_id = e.id AND le.license_id = @license_id
WHERE e.account_id = @account_id AND e.code = ANY(@codes::text[])
  AND (pe.id IS NOT NULL OR le.id IS NOT NULL);

-- name: CountMachinesByFingerprints :one
SELECT count(*) AS count
FROM lic.machines
WHERE license_id = @license_id AND fingerprint = ANY(@fingerprints::text[]);

-- name: ListLicenseEntitlementsOf :many
SELECT DISTINCT e.id, e.account_id, e.name, e.code, e.metadata, e.created_at, e.updated_at
FROM lic.entitlements e
LEFT JOIN lic.policy_entitlements pe ON pe.entitlement_id = e.id AND pe.policy_id = @policy_id
LEFT JOIN lic.license_entitlements le ON le.entitlement_id = e.id AND le.license_id = @license_id
WHERE e.account_id = @account_id AND (pe.id IS NOT NULL OR le.id IS NOT NULL)
ORDER BY e.created_at DESC, e.id DESC
LIMIT @limit OFFSET @offset;

-- name: CountLicenseEntitlementsOfUnion :one
SELECT count(DISTINCT e.id) AS count
FROM lic.entitlements e
LEFT JOIN lic.policy_entitlements pe ON pe.entitlement_id = e.id AND pe.policy_id = @policy_id
LEFT JOIN lic.license_entitlements le ON le.entitlement_id = e.id AND le.license_id = @license_id
WHERE e.account_id = @account_id AND (pe.id IS NOT NULL OR le.id IS NOT NULL);

-- name: GetLicenseEntitlementOf :one
SELECT e.id, e.account_id, e.name, e.code, e.metadata, e.created_at, e.updated_at
FROM lic.entitlements e
LEFT JOIN lic.policy_entitlements pe ON pe.entitlement_id = e.id AND pe.policy_id = @policy_id
LEFT JOIN lic.license_entitlements le ON le.entitlement_id = e.id AND le.license_id = @license_id
WHERE e.account_id = @account_id AND e.id = @id AND (pe.id IS NOT NULL OR le.id IS NOT NULL);
