-- backplane-lic's state: a 1:1 clone of the Keygen CE 1.7 licensing model,
-- scoped to singleplayer (no environments, releases, billing, SSO).
-- Every CHECK is named; FKs point only backwards (accounts first).
-- UUIDs are UUIDv4, like Keygen. Default ordering everywhere is
-- created_at DESC, id DESC (production default), hence the DESC indexes.
CREATE SCHEMA lic;

-- ---------------------------------------------------------------------------
-- accounts: the single account exists implicitly; the row is created at
-- startup from configuration with generated account keys.
CREATE TABLE lic.accounts (
  id                  uuid PRIMARY KEY,
  name                text,
  slug                text,
  api_version         text,
  protected           boolean NOT NULL DEFAULT false,
  -- v3 token digests are HMAC-SHA256 over this key.
  secret_key          text NOT NULL,
  -- RSA-2048 PEM (Keygen's private_key/public_key pair).
  private_key         text NOT NULL,
  public_key          text NOT NULL,
  -- ed25519 keys are stored as hex strings.
  ed25519_private_key text NOT NULL,
  ed25519_public_key  text NOT NULL,
  -- ECDSA P-256 PEM pair.
  ecdsa_private_key   text NOT NULL,
  ecdsa_public_key    text NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT accounts_slug_format CHECK (
    slug IS NULL OR slug ~ '^[a-z0-9][a-z0-9-]+$'
  ),
  CONSTRAINT accounts_api_version_format CHECK (
    api_version IS NULL OR api_version ~ '^[0-9]+\.[0-9]+$'
  )
);

CREATE UNIQUE INDEX accounts_slug_key ON lic.accounts (slug);
CREATE INDEX accounts_created_at_id_idx ON lic.accounts (created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- products.
CREATE TABLE lic.products (
  id                    uuid PRIMARY KEY,
  account_id            uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  name                  text,
  code                  text,
  -- OPEN | SECRET | CLOSED: who may see the product's licenses.
  distribution_strategy text,
  url                   text,
  -- EE release platforms the product publishes for; rendered verbatim.
  platforms             jsonb,
  metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT products_distribution_strategy CHECK (
    distribution_strategy IS NULL
    OR distribution_strategy IN ('LICENSED', 'OPEN', 'CLOSED')
  ),
  CONSTRAINT products_code_format CHECK (
    code IS NULL OR code ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]*$'
  )
);

-- Keygen's code uniqueness is case-insensitive (citext upstream).
CREATE UNIQUE INDEX products_account_code_key ON lic.products (account_id, lower(code));
CREATE INDEX products_account_created_at_id_idx ON lic.products (account_id, created_at DESC, id DESC);
CREATE INDEX products_code_idx ON lic.products (code);

-- ---------------------------------------------------------------------------
-- entitlements: features that policies/licenses attach.
CREATE TABLE lic.entitlements (
  id         uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  name       text NOT NULL,
  code       text NOT NULL,
  metadata   jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT entitlements_code_format CHECK (code ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]*$')
);

-- Keygen's code uniqueness is case-insensitive (citext upstream).
CREATE UNIQUE INDEX entitlements_account_code_key ON lic.entitlements (account_id, lower(code));
CREATE INDEX entitlements_created_at_id_idx ON lic.entitlements (created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- policies: licensing rules of one product.
CREATE TABLE lic.policies (
  id                            uuid PRIMARY KEY,
  account_id                    uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  product_id                    uuid NOT NULL REFERENCES lic.products (id) ON DELETE CASCADE,
  name                          text NOT NULL,
  duration                      integer,
  strict                        boolean NOT NULL DEFAULT false,
  floating                      boolean NOT NULL DEFAULT false,
  use_pool                      boolean NOT NULL DEFAULT false,
  max_machines                  integer,
  max_processes                 integer,
  max_users                     integer,
  max_cores                     integer,
  max_memory                    bigint,
  max_disk                      bigint,
  max_uses                      integer,
  machine_uniqueness_strategy   text,
  machine_matching_strategy     text,
  component_uniqueness_strategy text,
  component_matching_strategy   text,
  expiration_strategy           text,
  expiration_basis              text,
  renewal_basis                 text,
  transfer_strategy             text,
  authentication_strategy       text,
  machine_leasing_strategy      text,
  process_leasing_strategy      text,
  overage_strategy              text,
  scheme                        text,
  encrypted                     boolean NOT NULL DEFAULT false,
  protected                     boolean,
  require_product_scope         boolean NOT NULL DEFAULT false,
  require_policy_scope          boolean NOT NULL DEFAULT false,
  require_machine_scope         boolean NOT NULL DEFAULT false,
  require_fingerprint_scope     boolean NOT NULL DEFAULT false,
  require_components_scope      boolean NOT NULL DEFAULT false,
  require_user_scope            boolean NOT NULL DEFAULT false,
  require_checksum_scope        boolean NOT NULL DEFAULT false,
  require_version_scope         boolean NOT NULL DEFAULT false,
  require_check_in              boolean NOT NULL DEFAULT false,
  check_in_interval             text,
  check_in_interval_count       integer,
  heartbeat_duration            integer,
  heartbeat_cull_strategy       text,
  heartbeat_resurrection_strategy text,
  heartbeat_basis               text,
  require_heartbeat             boolean NOT NULL DEFAULT false,
  metadata                      jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at                    timestamptz NOT NULL DEFAULT now(),
  updated_at                    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT policies_duration_positive CHECK (duration IS NULL OR duration >= 1),
  CONSTRAINT policies_scheme_allowed CHECK (
    scheme IS NULL OR scheme IN (
      'LEGACY_ENCRYPT', 'RSA_2048_PKCS1_ENCRYPT', 'RSA_2048_PKCS1_SIGN',
      'RSA_2048_PKCS1_PSS_SIGN', 'RSA_2048_JWT_RS256', 'RSA_2048_PKCS1_SIGN_V2',
      'RSA_2048_PKCS1_PSS_SIGN_V2', 'ED25519_SIGN', 'ECDSA_P256_SIGN'
    )
  ),
  CONSTRAINT policies_machine_uniqueness_allowed CHECK (
    machine_uniqueness_strategy IS NULL OR machine_uniqueness_strategy IN (
      'UNIQUE_PER_ACCOUNT', 'UNIQUE_PER_PRODUCT', 'UNIQUE_PER_POLICY', 'UNIQUE_PER_LICENSE'
    )
  ),
  CONSTRAINT policies_component_uniqueness_allowed CHECK (
    component_uniqueness_strategy IS NULL OR component_uniqueness_strategy IN (
      'UNIQUE_PER_ACCOUNT', 'UNIQUE_PER_PRODUCT', 'UNIQUE_PER_POLICY',
      'UNIQUE_PER_LICENSE', 'UNIQUE_PER_MACHINE'
    )
  ),
  CONSTRAINT policies_machine_matching_allowed CHECK (
    machine_matching_strategy IS NULL
    OR machine_matching_strategy IN ('MATCH_ANY', 'MATCH_TWO', 'MATCH_MOST', 'MATCH_ALL')
  ),
  CONSTRAINT policies_component_matching_allowed CHECK (
    component_matching_strategy IS NULL
    OR component_matching_strategy IN ('MATCH_ANY', 'MATCH_TWO', 'MATCH_MOST', 'MATCH_ALL')
  ),
  CONSTRAINT policies_expiration_strategy_allowed CHECK (
    expiration_strategy IS NULL OR expiration_strategy IN (
      'RESTRICT_ACCESS', 'REVOKE_ACCESS', 'MAINTAIN_ACCESS', 'ALLOW_ACCESS'
    )
  ),
  CONSTRAINT policies_expiration_basis_allowed CHECK (
    expiration_basis IS NULL OR expiration_basis IN (
      'FROM_CREATION', 'FROM_FIRST_VALIDATION', 'FROM_FIRST_ACTIVATION',
      'FROM_FIRST_DOWNLOAD', 'FROM_FIRST_USE'
    )
  ),
  CONSTRAINT policies_renewal_basis_allowed CHECK (
    renewal_basis IS NULL OR renewal_basis IN ('FROM_EXPIRY', 'FROM_NOW', 'FROM_NOW_IF_EXPIRED')
  ),
  CONSTRAINT policies_transfer_strategy_allowed CHECK (
    transfer_strategy IS NULL OR transfer_strategy IN ('RESET_EXPIRY', 'KEEP_EXPIRY')
  ),
  CONSTRAINT policies_authentication_strategy_allowed CHECK (
    authentication_strategy IS NULL
    OR authentication_strategy IN ('TOKEN', 'LICENSE', 'SESSION', 'MIXED', 'NONE')
  ),
  CONSTRAINT policies_machine_leasing_allowed CHECK (
    machine_leasing_strategy IS NULL OR machine_leasing_strategy IN ('PER_LICENSE', 'PER_USER')
  ),
  CONSTRAINT policies_process_leasing_allowed CHECK (
    process_leasing_strategy IS NULL
    OR process_leasing_strategy IN ('PER_LICENSE', 'PER_MACHINE', 'PER_USER')
  ),
  CONSTRAINT policies_overage_allowed CHECK (
    overage_strategy IS NULL OR overage_strategy IN (
      'ALWAYS_ALLOW_OVERAGE', 'ALLOW_1_25X_OVERAGE', 'ALLOW_1_5X_OVERAGE',
      'ALLOW_2X_OVERAGE', 'NO_OVERAGE'
    )
  ),
  CONSTRAINT policies_heartbeat_cull_allowed CHECK (
    heartbeat_cull_strategy IS NULL OR heartbeat_cull_strategy IN ('DEACTIVATE_DEAD', 'KEEP_DEAD')
  ),
  CONSTRAINT policies_heartbeat_resurrection_allowed CHECK (
    heartbeat_resurrection_strategy IS NULL OR heartbeat_resurrection_strategy IN (
      'ALWAYS_REVIVE', '15_MINUTE_REVIVE', '10_MINUTE_REVIVE', '5_MINUTE_REVIVE',
      '2_MINUTE_REVIVE', '1_MINUTE_REVIVE', 'NO_REVIVE'
    )
  ),
  CONSTRAINT policies_heartbeat_basis_allowed CHECK (
    heartbeat_basis IS NULL OR heartbeat_basis IN ('FROM_CREATION', 'FROM_FIRST_PING')
  ),
  CONSTRAINT policies_check_in_interval_allowed CHECK (
    check_in_interval IS NULL OR check_in_interval IN ('day', 'week', 'month', 'year')
  ),
  CONSTRAINT policies_check_in_count_range CHECK (
    check_in_interval_count IS NULL OR check_in_interval_count BETWEEN 1 AND 365
  ),
  CONSTRAINT policies_heartbeat_duration_min CHECK (
    heartbeat_duration IS NULL OR heartbeat_duration >= 60
  ),
  CONSTRAINT policies_limits_nonnegative CHECK (
    (max_machines IS NULL OR max_machines >= 0)
    AND (max_processes IS NULL OR max_processes >= 0)
    AND (max_users IS NULL OR max_users >= 0)
    AND (max_cores IS NULL OR max_cores >= 0)
    AND (max_memory IS NULL OR max_memory >= 0)
    AND (max_disk IS NULL OR max_disk >= 0)
    AND (max_uses IS NULL OR max_uses >= 0)
  )
);

CREATE INDEX policies_account_created_at_id_idx ON lic.policies (account_id, created_at DESC, id DESC);
CREATE INDEX policies_product_id_idx ON lic.policies (product_id);
CREATE INDEX policies_require_heartbeat_idx ON lic.policies (require_heartbeat) WHERE require_heartbeat;

-- ---------------------------------------------------------------------------
-- policy_entitlements: which entitlements a policy grants its licenses.
CREATE TABLE lic.policy_entitlements (
  id             uuid PRIMARY KEY,
  account_id     uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  policy_id      uuid NOT NULL REFERENCES lic.policies (id) ON DELETE CASCADE,
  entitlement_id uuid NOT NULL REFERENCES lic.entitlements (id) ON DELETE CASCADE,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX policy_entitlements_ids_key ON lic.policy_entitlements (account_id, policy_id, entitlement_id);
CREATE INDEX policy_entitlements_policy_idx ON lic.policy_entitlements (policy_id);
CREATE INDEX policy_entitlements_entitlement_idx ON lic.policy_entitlements (entitlement_id);

-- ---------------------------------------------------------------------------
-- keys: the license-key pool of a policy (use_pool policies pop keys).
CREATE TABLE lic.keys (
  id         uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  policy_id  uuid REFERENCES lic.policies (id) ON DELETE CASCADE,
  key        text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT keys_nonempty CHECK (length(btrim(key)) > 0)
);

CREATE UNIQUE INDEX keys_account_key_key ON lic.keys (account_id, key);
CREATE INDEX keys_policy_idx ON lic.keys (policy_id);
CREATE INDEX keys_created_at_id_idx ON lic.keys (created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- groups: user grouping with limits.
CREATE TABLE lic.groups (
  id           uuid PRIMARY KEY,
  account_id   uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  name         text,
  max_users    integer,
  max_licenses integer,
  max_machines integer,
  metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT groups_limits_nonnegative CHECK (
    (max_users IS NULL OR max_users >= 0)
    AND (max_licenses IS NULL OR max_licenses >= 0)
    AND (max_machines IS NULL OR max_machines >= 0)
  )
);

CREATE INDEX groups_account_created_at_id_idx ON lic.groups (account_id, created_at DESC, id DESC);

-- group_owners: which users administer a group. Defined after users so
-- every FK points backwards.

-- ---------------------------------------------------------------------------
-- users: license owners and admin bearers. Keygen keeps roles in separate
-- role tables; singleplayer needs one role per user, so it lives here.
CREATE TABLE lic.users (
  id            uuid PRIMARY KEY,
  account_id    uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  first_name    text,
  last_name     text,
  email         text,
  password_digest text,
  -- user | admin | developer | read_only | sales_agent | support_agent.
  role          text NOT NULL DEFAULT 'user',
  permissions   jsonb NOT NULL DEFAULT '[]'::jsonb,
  group_id      uuid REFERENCES lic.groups (id) ON DELETE SET NULL,
  metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
  password_reset_token text,
  password_reset_sent_at timestamptz,
  banned_at     timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT users_role_allowed CHECK (
    role IN ('user', 'admin', 'developer', 'read_only', 'sales_agent', 'support_agent')
  ),
  CONSTRAINT users_email_format CHECK (
    email IS NULL OR email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
  )
);

CREATE UNIQUE INDEX users_account_email_key ON lic.users (account_id, email);
CREATE INDEX users_created_at_id_idx ON lic.users (created_at DESC, id DESC);
CREATE INDEX users_group_idx ON lic.users (group_id);

CREATE TABLE lic.group_owners (
  id         uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  group_id   uuid NOT NULL REFERENCES lic.groups (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES lic.users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX group_owners_group_user_key ON lic.group_owners (group_id, user_id);
CREATE INDEX group_owners_user_idx ON lic.group_owners (user_id);
CREATE INDEX group_owners_account_idx ON lic.group_owners (account_id);

-- ---------------------------------------------------------------------------
-- licenses.
CREATE TABLE lic.licenses (
  id                       uuid PRIMARY KEY,
  account_id               uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  product_id               uuid NOT NULL REFERENCES lic.products (id) ON DELETE CASCADE,
  policy_id                uuid NOT NULL REFERENCES lic.policies (id) ON DELETE CASCADE,
  user_id                  uuid REFERENCES lic.users (id) ON DELETE SET NULL,
  group_id                 uuid REFERENCES lic.groups (id) ON DELETE SET NULL,
  name                     text,
  key                      text NOT NULL,
  expiry                   timestamptz,
  uses                     integer NOT NULL DEFAULT 0,
  suspended                boolean NOT NULL DEFAULT false,
  protected                boolean,
  metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb,
  -- Limits inherited from the policy, overridable per license.
  max_machines_override    integer,
  max_processes_override   integer,
  max_users_override       integer,
  max_cores_override       integer,
  max_memory_override      bigint,
  max_disk_override        bigint,
  max_uses_override        integer,
  -- Denormalized counters (kept by the app, like Keygen's counter caches).
  machines_count           integer NOT NULL DEFAULT 0,
  machines_core_count      integer NOT NULL DEFAULT 0,
  machines_memory_count    bigint NOT NULL DEFAULT 0,
  machines_disk_count      bigint NOT NULL DEFAULT 0,
  license_users_count      integer NOT NULL DEFAULT 0,
  last_validated_at        timestamptz,
  last_validated_checksum  text,
  last_validated_version   text,
  last_check_in_at         timestamptz,
  last_check_out_at        timestamptz,
  -- One-shot guards so the periodic expiring-soon/expired notifications
  -- fire exactly once per license.
  last_expiring_soon_event_sent_at timestamptz,
  last_expiration_event_sent_at    timestamptz,
  last_check_in_soon_event_sent_at timestamptz,
  last_check_in_event_sent_at      timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT licenses_uses_nonnegative CHECK (uses >= 0),
  CONSTRAINT licenses_counts_nonnegative CHECK (
    machines_count >= 0
    AND machines_core_count >= 0
    AND machines_memory_count >= 0
    AND machines_disk_count >= 0
    AND license_users_count >= 0
  ),
  CONSTRAINT licenses_limits_nonnegative CHECK (
    (max_machines_override IS NULL OR max_machines_override >= 0)
    AND (max_processes_override IS NULL OR max_processes_override >= 0)
    AND (max_users_override IS NULL OR max_users_override >= 0)
    AND (max_cores_override IS NULL OR max_cores_override >= 0)
    AND (max_memory_override IS NULL OR max_memory_override >= 0)
    AND (max_disk_override IS NULL OR max_disk_override >= 0)
    AND (max_uses_override IS NULL OR max_uses_override >= 0)
  )
);

CREATE UNIQUE INDEX licenses_account_key_key ON lic.licenses (account_id, key);
CREATE INDEX licenses_key_idx ON lic.licenses (key);
CREATE INDEX licenses_account_created_at_id_idx ON lic.licenses (account_id, created_at DESC, id DESC);
CREATE INDEX licenses_product_idx ON lic.licenses (product_id);
CREATE INDEX licenses_policy_idx ON lic.licenses (policy_id);
CREATE INDEX licenses_user_idx ON lic.licenses (user_id);
CREATE INDEX licenses_group_idx ON lic.licenses (group_id);
CREATE INDEX licenses_last_validated_idx ON lic.licenses (last_validated_at);
CREATE INDEX licenses_expiry_idx ON lic.licenses (expiry) WHERE expiry IS NOT NULL;

-- license_entitlements: entitlements granted to a license beyond its policy.
CREATE TABLE lic.license_entitlements (
  id             uuid PRIMARY KEY,
  account_id     uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  license_id     uuid NOT NULL REFERENCES lic.licenses (id) ON DELETE CASCADE,
  entitlement_id uuid NOT NULL REFERENCES lic.entitlements (id) ON DELETE CASCADE,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX license_entitlements_ids_key ON lic.license_entitlements (account_id, license_id, entitlement_id);
CREATE INDEX license_entitlements_license_idx ON lic.license_entitlements (license_id);
CREATE INDEX license_entitlements_entitlement_idx ON lic.license_entitlements (entitlement_id);

-- license_users: users attached to a license beyond the owner.
CREATE TABLE lic.license_users (
  id         uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  license_id uuid NOT NULL REFERENCES lic.licenses (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES lic.users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX license_users_ids_key ON lic.license_users (account_id, license_id, user_id);
CREATE INDEX license_users_license_idx ON lic.license_users (license_id);
CREATE INDEX license_users_user_idx ON lic.license_users (user_id);

-- ---------------------------------------------------------------------------
-- machines.
CREATE TABLE lic.machines (
  id                     uuid PRIMARY KEY,
  account_id             uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  license_id             uuid NOT NULL REFERENCES lic.licenses (id) ON DELETE CASCADE,
  policy_id              uuid NOT NULL REFERENCES lic.policies (id),
  owner_id               uuid REFERENCES lic.users (id) ON DELETE SET NULL,
  group_id               uuid REFERENCES lic.groups (id) ON DELETE SET NULL,
  name                   text,
  fingerprint            text NOT NULL,
  ip                     text,
  hostname               text,
  platform               text,
  cores                  integer,
  memory                 bigint,
  disk                   bigint,
  max_processes_override integer,
  metadata               jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_heartbeat_at      timestamptz,
  last_check_out_at      timestamptz,
  last_death_event_sent_at timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT machines_fingerprint_nonempty CHECK (length(btrim(fingerprint)) > 0),
  CONSTRAINT machines_resources_nonnegative CHECK (
    (cores IS NULL OR cores >= 0)
    AND (memory IS NULL OR memory >= 0)
    AND (disk IS NULL OR disk >= 0)
    AND (max_processes_override IS NULL OR max_processes_override >= 0)
  )
);

CREATE UNIQUE INDEX machines_license_fingerprint_key ON lic.machines (license_id, fingerprint);
CREATE INDEX machines_fingerprint_idx ON lic.machines (fingerprint);
CREATE INDEX machines_account_created_at_id_idx ON lic.machines (account_id, created_at DESC, id DESC);
CREATE INDEX machines_license_idx ON lic.machines (license_id);
CREATE INDEX machines_owner_idx ON lic.machines (owner_id);
CREATE INDEX machines_last_heartbeat_idx ON lic.machines (last_heartbeat_at);

-- machine_components.
CREATE TABLE lic.machine_components (
  id          uuid PRIMARY KEY,
  account_id  uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  machine_id  uuid NOT NULL REFERENCES lic.machines (id) ON DELETE CASCADE,
  name        text NOT NULL,
  fingerprint text NOT NULL,
  metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT machine_components_names_nonempty CHECK (length(btrim(name)) > 0),
  CONSTRAINT machine_components_fingerprint_nonempty CHECK (length(btrim(fingerprint)) > 0)
);

CREATE UNIQUE INDEX machine_components_machine_fingerprint_key ON lic.machine_components (machine_id, fingerprint);
CREATE INDEX machine_components_fingerprint_idx ON lic.machine_components (fingerprint);
CREATE INDEX machine_components_account_created_at_id_idx ON lic.machine_components (account_id, created_at DESC, id DESC);

-- machine_processes.
CREATE TABLE lic.machine_processes (
  id                     uuid PRIMARY KEY,
  account_id             uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  machine_id             uuid NOT NULL REFERENCES lic.machines (id) ON DELETE CASCADE,
  pid                    text NOT NULL,
  metadata               jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_heartbeat_at      timestamptz NOT NULL,
  last_death_event_sent_at timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT machine_processes_pid_nonempty CHECK (length(btrim(pid)) > 0)
);

CREATE UNIQUE INDEX machine_processes_machine_pid_key ON lic.machine_processes (machine_id, pid);
CREATE INDEX machine_processes_last_heartbeat_idx ON lic.machine_processes (last_heartbeat_at);
CREATE INDEX machine_processes_account_created_at_id_idx ON lic.machine_processes (account_id, created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- tokens: API credentials. bearer_type/bearer_id/bearer_role is the
-- polymorphic bearer (user, product, license). Only the digest is stored.
CREATE TABLE lic.tokens (
  id                uuid PRIMARY KEY,
  account_id        uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  digest            text NOT NULL,
  bearer_id         uuid,
  bearer_type       text,
  bearer_role       text,
  kind              text,
  name              text,
  expiry            timestamptz,
  permissions       jsonb NOT NULL DEFAULT '["*"]'::jsonb,
  -- Activation-token counters.
  max_activations   integer,
  activations       integer NOT NULL DEFAULT 0,
  max_deactivations integer,
  deactivations     integer NOT NULL DEFAULT 0,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT tokens_bearer_type_allowed CHECK (
    bearer_type IS NULL OR bearer_type IN ('User', 'Product', 'License')
  ),
  CONSTRAINT tokens_digest_nonempty CHECK (length(btrim(digest)) > 0),
  CONSTRAINT tokens_activations_range CHECK (
    (max_activations IS NULL OR max_activations >= 0)
    AND (max_deactivations IS NULL OR max_deactivations >= 0)
    AND activations >= 0
    AND deactivations >= 0
  )
);

CREATE UNIQUE INDEX tokens_digest_key ON lic.tokens (digest);
CREATE INDEX tokens_bearer_idx ON lic.tokens (bearer_id, bearer_type);
CREATE INDEX tokens_account_created_at_id_idx ON lic.tokens (account_id, created_at DESC, id DESC);
CREATE INDEX tokens_expiry_idx ON lic.tokens (expiry) WHERE expiry IS NOT NULL;

-- second_factors: TOTP secrets of users.
CREATE TABLE lic.second_factors (
  id              uuid PRIMARY KEY,
  account_id      uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES lic.users (id) ON DELETE CASCADE,
  secret          text NOT NULL,
  enabled         boolean NOT NULL DEFAULT false,
  last_verified_at timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX second_factors_user_key ON lic.second_factors (user_id);
CREATE UNIQUE INDEX second_factors_secret_key ON lic.second_factors (secret);

-- ---------------------------------------------------------------------------
-- webhook_endpoints.
CREATE TABLE lic.webhook_endpoints (
  id                   uuid PRIMARY KEY,
  account_id           uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  -- No FK: the reference stores the relationship id verbatim (a
  -- nonexistent product is accepted on create).
  product_id           uuid,
  url                  text NOT NULL,
  subscriptions        jsonb NOT NULL DEFAULT '["*"]'::jsonb,
  signature_algorithm  text NOT NULL DEFAULT 'ed25519',
  api_version          text,
  -- Keygen disables broken endpoints by clearing subscriptions.
  disabled_at          timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT webhook_endpoints_signature_algorithm_allowed CHECK (
    signature_algorithm IN ('ed25519', 'rsa-pss-sha256', 'rsa-sha256', 'ecdsa-p256')
  ),
  CONSTRAINT webhook_endpoints_url_https CHECK (url ~ '^https://'),
  CONSTRAINT webhook_endpoints_api_version_format CHECK (
    api_version IS NULL OR api_version ~ '^[0-9]+\.[0-9]+$'
  )
);

CREATE INDEX webhook_endpoints_account_created_at_id_idx ON lic.webhook_endpoints (account_id, created_at DESC, id DESC);
CREATE INDEX webhook_endpoints_product_idx ON lic.webhook_endpoints (product_id);

-- webhook_events: the outbox. Domain writes insert rows in the same
-- transaction; a worker claims due rows with FOR UPDATE SKIP LOCKED and
-- delivers them. Keygen's sidekiq jid is meaningless here — attempts and
-- next_attempt_at replace sidekiq's retry bookkeeping.
CREATE TABLE lic.webhook_events (
  id                  uuid PRIMARY KEY,
  account_id          uuid NOT NULL REFERENCES lic.accounts (id) ON DELETE CASCADE,
  -- No FK: the endpoint row may be destroyed between the broadcast and
  -- the outbox write (the reference's association is optional).
  webhook_endpoint_id uuid,
  -- URL snapshot copied from the endpoint at creation time.
  endpoint            text NOT NULL,
  event               text NOT NULL,
  -- Serialized JSON:API document of the event's subject, as a string.
  payload             text NOT NULL,
  api_version         text,
  idempotency_token   text NOT NULL,
  -- DELIVERING | DELIVERED | FAILING | FAILED.
  status              text NOT NULL DEFAULT 'DELIVERING',
  attempts            integer NOT NULL DEFAULT 0,
  next_attempt_at     timestamptz,
  last_response_code  integer,
  last_response_body  text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT webhook_events_status_allowed CHECK (
    status IN ('DELIVERING', 'DELIVERED', 'FAILING', 'FAILED')
  ),
  CONSTRAINT webhook_events_attempts_nonnegative CHECK (attempts >= 0)
);

CREATE INDEX webhook_events_account_created_at_id_idx ON lic.webhook_events (account_id, created_at DESC, id DESC);
CREATE INDEX webhook_events_endpoint_idx ON lic.webhook_events (webhook_endpoint_id);
CREATE INDEX webhook_events_due_idx ON lic.webhook_events (next_attempt_at)
  WHERE status IN ('DELIVERING', 'FAILING');
CREATE INDEX webhook_events_idempotency_idx ON lic.webhook_events (idempotency_token);
