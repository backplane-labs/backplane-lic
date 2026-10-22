-- Policies (internal/app/policies).

-- name: GetPolicyByID :one
SELECT id, account_id, product_id, name, duration, strict, floating,
  use_pool, max_machines, max_processes, max_users, max_cores, max_memory,
  max_disk, max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat,
  metadata, created_at, updated_at
FROM lic.policies
WHERE account_id = @account_id AND id = @id;

-- name: InsertPolicy :exec
INSERT INTO lic.policies (
  id, account_id, product_id, name, duration, strict, floating, use_pool,
  max_machines, max_processes, max_users, max_cores, max_memory, max_disk,
  max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat, metadata
) VALUES (
  @id, @account_id, @product_id, @name, @duration, @strict, @floating, @use_pool,
  @max_machines, @max_processes, @max_users, @max_cores, @max_memory, @max_disk,
  @max_uses, @machine_uniqueness_strategy, @machine_matching_strategy,
  @component_uniqueness_strategy, @component_matching_strategy,
  @expiration_strategy, @expiration_basis, @renewal_basis, @transfer_strategy,
  @authentication_strategy, @machine_leasing_strategy, @process_leasing_strategy,
  @overage_strategy, @scheme, @encrypted, @protected, @require_product_scope,
  @require_policy_scope, @require_machine_scope, @require_fingerprint_scope,
  @require_components_scope, @require_user_scope, @require_checksum_scope,
  @require_version_scope, @require_check_in, @check_in_interval,
  @check_in_interval_count, @heartbeat_duration, @heartbeat_cull_strategy,
  @heartbeat_resurrection_strategy, @heartbeat_basis, @require_heartbeat, @metadata
);

-- name: UpdatePolicy :exec
UPDATE lic.policies SET
  name = @name,
  duration = @duration,
  strict = @strict,
  floating = @floating,
  max_machines = @max_machines,
  max_processes = @max_processes,
  max_users = @max_users,
  max_cores = @max_cores,
  max_memory = @max_memory,
  max_disk = @max_disk,
  max_uses = @max_uses,
  machine_uniqueness_strategy = @machine_uniqueness_strategy,
  machine_matching_strategy = @machine_matching_strategy,
  component_uniqueness_strategy = @component_uniqueness_strategy,
  component_matching_strategy = @component_matching_strategy,
  expiration_strategy = @expiration_strategy,
  expiration_basis = @expiration_basis,
  renewal_basis = @renewal_basis,
  transfer_strategy = @transfer_strategy,
  authentication_strategy = @authentication_strategy,
  machine_leasing_strategy = @machine_leasing_strategy,
  process_leasing_strategy = @process_leasing_strategy,
  overage_strategy = @overage_strategy,
  protected = @protected,
  require_product_scope = @require_product_scope,
  require_policy_scope = @require_policy_scope,
  require_machine_scope = @require_machine_scope,
  require_fingerprint_scope = @require_fingerprint_scope,
  require_components_scope = @require_components_scope,
  require_user_scope = @require_user_scope,
  require_checksum_scope = @require_checksum_scope,
  require_version_scope = @require_version_scope,
  require_check_in = @require_check_in,
  check_in_interval = @check_in_interval,
  check_in_interval_count = @check_in_interval_count,
  heartbeat_duration = @heartbeat_duration,
  heartbeat_cull_strategy = @heartbeat_cull_strategy,
  heartbeat_resurrection_strategy = @heartbeat_resurrection_strategy,
  heartbeat_basis = @heartbeat_basis,
  require_heartbeat = @require_heartbeat,
  metadata = @metadata,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeletePolicy :execrows
DELETE FROM lic.policies
WHERE account_id = @account_id AND id = @id;

-- The admin-side list (user and license bearers load the same page and
-- fail the post-pagination check). @product_id filters by product.
-- name: ListPoliciesDesc :many
SELECT id, account_id, product_id, name, duration, strict, floating,
  use_pool, max_machines, max_processes, max_users, max_cores, max_memory,
  max_disk, max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat,
  metadata, created_at, updated_at
FROM lic.policies
WHERE account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR product_id = @filter_product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.policies p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListPoliciesAsc :many
SELECT id, account_id, product_id, name, duration, strict, floating,
  use_pool, max_machines, max_processes, max_users, max_cores, max_memory,
  max_disk, max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat,
  metadata, created_at, updated_at
FROM lic.policies
WHERE account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR product_id = @filter_product_id)
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.policies p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountPolicies :one
SELECT count(*) FROM lic.policies
WHERE account_id = @account_id
  AND (@filter_product_id::uuid IS NULL OR product_id = @filter_product_id);

-- The product-bearer scope (Policy.for_product): the product's policies.
-- name: ListPoliciesForProductDesc :many
SELECT id, account_id, product_id, name, duration, strict, floating,
  use_pool, max_machines, max_processes, max_users, max_cores, max_memory,
  max_disk, max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat,
  metadata, created_at, updated_at
FROM lic.policies
WHERE account_id = @account_id AND product_id = @bearer_product_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) < (
      SELECT p2.created_at, p2.id FROM lic.policies p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: ListPoliciesForProductAsc :many
SELECT id, account_id, product_id, name, duration, strict, floating,
  use_pool, max_machines, max_processes, max_users, max_cores, max_memory,
  max_disk, max_uses, machine_uniqueness_strategy, machine_matching_strategy,
  component_uniqueness_strategy, component_matching_strategy,
  expiration_strategy, expiration_basis, renewal_basis, transfer_strategy,
  authentication_strategy, machine_leasing_strategy, process_leasing_strategy,
  overage_strategy, scheme, encrypted, protected, require_product_scope,
  require_policy_scope, require_machine_scope, require_fingerprint_scope,
  require_components_scope, require_user_scope, require_checksum_scope,
  require_version_scope, require_check_in, check_in_interval,
  check_in_interval_count, heartbeat_duration, heartbeat_cull_strategy,
  heartbeat_resurrection_strategy, heartbeat_basis, require_heartbeat,
  metadata, created_at, updated_at
FROM lic.policies
WHERE account_id = @account_id AND product_id = @bearer_product_id
  AND (
    @cursor_id::uuid IS NULL
    OR (created_at, id) > (
      SELECT p2.created_at, p2.id FROM lic.policies p2
      WHERE p2.account_id = @account_id AND p2.id = @cursor_id
    )
  )
ORDER BY created_at ASC, id ASC
LIMIT @limit OFFSET @offset;

-- name: CountPoliciesForProduct :one
SELECT count(*) FROM lic.policies
WHERE account_id = @account_id AND product_id = @bearer_product_id;

-- The policies of a user's licenses (the policies-index post-check and
-- the user show check).
-- name: UserPolicyIDs :many
SELECT DISTINCT po.id
FROM lic.policies po
JOIN lic.licenses l ON l.policy_id = po.id
WHERE l.account_id = @account_id
  AND (
    l.user_id = @user_id
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = l.id AND lu.user_id = @user_id
    )
  );

-- The license bearer's own policy (License#policy): the policies-index
-- post-check and the show check.
-- name: GetPolicyByLicense :one
SELECT po.id, po.account_id, po.product_id, po.name, po.duration, po.strict,
  po.floating, po.use_pool, po.max_machines, po.max_processes, po.max_users,
  po.max_cores, po.max_memory, po.max_disk, po.max_uses,
  po.machine_uniqueness_strategy, po.machine_matching_strategy,
  po.component_uniqueness_strategy, po.component_matching_strategy,
  po.expiration_strategy, po.expiration_basis, po.renewal_basis,
  po.transfer_strategy, po.authentication_strategy, po.machine_leasing_strategy,
  po.process_leasing_strategy, po.overage_strategy, po.scheme, po.encrypted,
  po.protected, po.require_product_scope, po.require_policy_scope,
  po.require_machine_scope, po.require_fingerprint_scope,
  po.require_components_scope, po.require_user_scope, po.require_checksum_scope,
  po.require_version_scope, po.require_check_in, po.check_in_interval,
  po.check_in_interval_count, po.heartbeat_duration, po.heartbeat_cull_strategy,
  po.heartbeat_resurrection_strategy, po.heartbeat_basis, po.require_heartbeat,
  po.metadata, po.created_at, po.updated_at
FROM lic.policies po
JOIN lic.licenses l ON l.policy_id = po.id
WHERE l.account_id = @account_id AND l.id = @license_id;
