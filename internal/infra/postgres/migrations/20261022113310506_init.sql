-- sqld:up
CREATE SCHEMA IF NOT EXISTS "lic";
CREATE TABLE "lic"."accounts" (
  "id" uuid NOT NULL,
  "name" text,
  "slug" text,
  "api_version" text,
  "protected" bool NOT NULL DEFAULT false,
  "secret_key" text NOT NULL,
  "private_key" text NOT NULL,
  "public_key" text NOT NULL,
  "ed25519_private_key" text NOT NULL,
  "ed25519_public_key" text NOT NULL,
  "ecdsa_private_key" text NOT NULL,
  "ecdsa_public_key" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."entitlements" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "name" text NOT NULL,
  "code" text NOT NULL,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."group_owners" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "group_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."groups" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "name" text,
  "max_users" int4,
  "max_licenses" int4,
  "max_machines" int4,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."keys" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "policy_id" uuid,
  "key" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."license_entitlements" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "license_id" uuid NOT NULL,
  "entitlement_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."license_users" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "license_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."licenses" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "product_id" uuid NOT NULL,
  "policy_id" uuid NOT NULL,
  "user_id" uuid,
  "group_id" uuid,
  "name" text,
  "key" text NOT NULL,
  "expiry" timestamptz,
  "uses" int4 NOT NULL DEFAULT 0,
  "suspended" bool NOT NULL DEFAULT false,
  "protected" bool,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "max_machines_override" int4,
  "max_processes_override" int4,
  "max_users_override" int4,
  "max_cores_override" int4,
  "max_memory_override" int8,
  "max_disk_override" int8,
  "max_uses_override" int4,
  "machines_count" int4 NOT NULL DEFAULT 0,
  "machines_core_count" int4 NOT NULL DEFAULT 0,
  "machines_memory_count" int8 NOT NULL DEFAULT 0,
  "machines_disk_count" int8 NOT NULL DEFAULT 0,
  "license_users_count" int4 NOT NULL DEFAULT 0,
  "last_validated_at" timestamptz,
  "last_validated_checksum" text,
  "last_validated_version" text,
  "last_check_in_at" timestamptz,
  "last_check_out_at" timestamptz,
  "last_expiring_soon_event_sent_at" timestamptz,
  "last_expiration_event_sent_at" timestamptz,
  "last_check_in_soon_event_sent_at" timestamptz,
  "last_check_in_event_sent_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."machine_components" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "machine_id" uuid NOT NULL,
  "name" text NOT NULL,
  "fingerprint" text NOT NULL,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."machine_processes" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "machine_id" uuid NOT NULL,
  "pid" text NOT NULL,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "last_heartbeat_at" timestamptz NOT NULL,
  "last_death_event_sent_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."machines" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "license_id" uuid NOT NULL,
  "policy_id" uuid NOT NULL,
  "owner_id" uuid,
  "group_id" uuid,
  "name" text,
  "fingerprint" text NOT NULL,
  "ip" text,
  "hostname" text,
  "platform" text,
  "cores" int4,
  "memory" int8,
  "disk" int8,
  "max_processes_override" int4,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "last_heartbeat_at" timestamptz,
  "last_check_out_at" timestamptz,
  "last_death_event_sent_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."policies" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "product_id" uuid NOT NULL,
  "name" text NOT NULL,
  "duration" int4,
  "strict" bool NOT NULL DEFAULT false,
  "floating" bool NOT NULL DEFAULT false,
  "use_pool" bool NOT NULL DEFAULT false,
  "max_machines" int4,
  "max_processes" int4,
  "max_users" int4,
  "max_cores" int4,
  "max_memory" int8,
  "max_disk" int8,
  "max_uses" int4,
  "machine_uniqueness_strategy" text,
  "machine_matching_strategy" text,
  "component_uniqueness_strategy" text,
  "component_matching_strategy" text,
  "expiration_strategy" text,
  "expiration_basis" text,
  "renewal_basis" text,
  "transfer_strategy" text,
  "authentication_strategy" text,
  "machine_leasing_strategy" text,
  "process_leasing_strategy" text,
  "overage_strategy" text,
  "scheme" text,
  "encrypted" bool NOT NULL DEFAULT false,
  "protected" bool,
  "require_product_scope" bool NOT NULL DEFAULT false,
  "require_policy_scope" bool NOT NULL DEFAULT false,
  "require_machine_scope" bool NOT NULL DEFAULT false,
  "require_fingerprint_scope" bool NOT NULL DEFAULT false,
  "require_components_scope" bool NOT NULL DEFAULT false,
  "require_user_scope" bool NOT NULL DEFAULT false,
  "require_checksum_scope" bool NOT NULL DEFAULT false,
  "require_version_scope" bool NOT NULL DEFAULT false,
  "require_check_in" bool NOT NULL DEFAULT false,
  "check_in_interval" text,
  "check_in_interval_count" int4,
  "heartbeat_duration" int4,
  "heartbeat_cull_strategy" text,
  "heartbeat_resurrection_strategy" text,
  "heartbeat_basis" text,
  "require_heartbeat" bool NOT NULL DEFAULT false,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."policy_entitlements" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "policy_id" uuid NOT NULL,
  "entitlement_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."products" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "name" text,
  "code" text,
  "distribution_strategy" text,
  "url" text,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."second_factors" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "secret" text NOT NULL,
  "enabled" bool NOT NULL DEFAULT false,
  "last_verified_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."tokens" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "digest" text NOT NULL,
  "bearer_id" uuid,
  "bearer_type" text,
  "bearer_role" text,
  "kind" text,
  "name" text,
  "expiry" timestamptz,
  "permissions" jsonb NOT NULL DEFAULT '["*"]'::jsonb,
  "max_activations" int4,
  "activations" int4 NOT NULL DEFAULT 0,
  "max_deactivations" int4,
  "deactivations" int4 NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."users" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "first_name" text,
  "last_name" text,
  "email" text,
  "password_digest" text,
  "role" text NOT NULL DEFAULT 'user'::text,
  "permissions" jsonb NOT NULL DEFAULT '[]'::jsonb,
  "group_id" uuid,
  "metadata" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "password_reset_token" text,
  "password_reset_sent_at" timestamptz,
  "banned_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."webhook_endpoints" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "product_id" uuid,
  "url" text NOT NULL,
  "subscriptions" jsonb NOT NULL DEFAULT '["*"]'::jsonb,
  "signature_algorithm" text NOT NULL DEFAULT 'ed25519'::text,
  "api_version" text,
  "disabled_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
CREATE TABLE "lic"."webhook_events" (
  "id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "webhook_endpoint_id" uuid,
  "endpoint" text NOT NULL,
  "event" text NOT NULL,
  "payload" text NOT NULL,
  "api_version" text,
  "idempotency_token" text NOT NULL,
  "status" text NOT NULL DEFAULT 'DELIVERING'::text,
  "attempts" int4 NOT NULL DEFAULT 0,
  "next_attempt_at" timestamptz,
  "last_response_code" int4,
  "last_response_body" text,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
ALTER TABLE "lic"."accounts" ADD CONSTRAINT "accounts_api_version_format" CHECK (api_version IS NULL OR api_version ~ '^[0-9]+\.[0-9]+$'::text);
ALTER TABLE "lic"."accounts" ADD CONSTRAINT "accounts_slug_format" CHECK (slug IS NULL OR slug ~ '^[a-z0-9][a-z0-9-]+$'::text);
ALTER TABLE "lic"."entitlements" ADD CONSTRAINT "entitlements_code_format" CHECK (code ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]*$'::text);
ALTER TABLE "lic"."groups" ADD CONSTRAINT "groups_limits_nonnegative" CHECK ((max_users IS NULL OR max_users >= 0) AND (max_licenses IS NULL OR max_licenses >= 0) AND (max_machines IS NULL OR max_machines >= 0));
ALTER TABLE "lic"."keys" ADD CONSTRAINT "keys_nonempty" CHECK (length(btrim(key)) > 0);
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_counts_nonnegative" CHECK (machines_count >= 0 AND machines_core_count >= 0 AND machines_memory_count >= 0 AND machines_disk_count >= 0 AND license_users_count >= 0);
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_limits_nonnegative" CHECK ((max_machines_override IS NULL OR max_machines_override >= 0) AND (max_processes_override IS NULL OR max_processes_override >= 0) AND (max_users_override IS NULL OR max_users_override >= 0) AND (max_cores_override IS NULL OR max_cores_override >= 0) AND (max_memory_override IS NULL OR max_memory_override >= 0) AND (max_disk_override IS NULL OR max_disk_override >= 0) AND (max_uses_override IS NULL OR max_uses_override >= 0));
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_uses_nonnegative" CHECK (uses >= 0);
ALTER TABLE "lic"."machine_components" ADD CONSTRAINT "machine_components_fingerprint_nonempty" CHECK (length(btrim(fingerprint)) > 0);
ALTER TABLE "lic"."machine_components" ADD CONSTRAINT "machine_components_names_nonempty" CHECK (length(btrim(name)) > 0);
ALTER TABLE "lic"."machine_processes" ADD CONSTRAINT "machine_processes_pid_nonempty" CHECK (length(btrim(pid)) > 0);
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_fingerprint_nonempty" CHECK (length(btrim(fingerprint)) > 0);
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_resources_nonnegative" CHECK ((cores IS NULL OR cores >= 0) AND (memory IS NULL OR memory >= 0) AND (disk IS NULL OR disk >= 0) AND (max_processes_override IS NULL OR max_processes_override >= 0));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_authentication_strategy_allowed" CHECK (authentication_strategy IS NULL OR (authentication_strategy = ANY (ARRAY['TOKEN'::text, 'LICENSE'::text, 'SESSION'::text, 'MIXED'::text, 'NONE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_check_in_count_range" CHECK (check_in_interval_count IS NULL OR check_in_interval_count >= 1 AND check_in_interval_count <= 365);
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_check_in_interval_allowed" CHECK (check_in_interval IS NULL OR (check_in_interval = ANY (ARRAY['day'::text, 'week'::text, 'month'::text, 'year'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_component_matching_allowed" CHECK (component_matching_strategy IS NULL OR (component_matching_strategy = ANY (ARRAY['MATCH_ANY'::text, 'MATCH_TWO'::text, 'MATCH_MOST'::text, 'MATCH_ALL'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_component_uniqueness_allowed" CHECK (component_uniqueness_strategy IS NULL OR (component_uniqueness_strategy = ANY (ARRAY['UNIQUE_PER_ACCOUNT'::text, 'UNIQUE_PER_PRODUCT'::text, 'UNIQUE_PER_POLICY'::text, 'UNIQUE_PER_LICENSE'::text, 'UNIQUE_PER_MACHINE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_duration_positive" CHECK (duration IS NULL OR duration >= 1);
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_expiration_basis_allowed" CHECK (expiration_basis IS NULL OR (expiration_basis = ANY (ARRAY['FROM_CREATION'::text, 'FROM_FIRST_VALIDATION'::text, 'FROM_FIRST_ACTIVATION'::text, 'FROM_FIRST_DOWNLOAD'::text, 'FROM_FIRST_USE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_expiration_strategy_allowed" CHECK (expiration_strategy IS NULL OR (expiration_strategy = ANY (ARRAY['RESTRICT_ACCESS'::text, 'REVOKE_ACCESS'::text, 'MAINTAIN_ACCESS'::text, 'ALLOW_ACCESS'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_heartbeat_basis_allowed" CHECK (heartbeat_basis IS NULL OR (heartbeat_basis = ANY (ARRAY['FROM_CREATION'::text, 'FROM_FIRST_PING'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_heartbeat_cull_allowed" CHECK (heartbeat_cull_strategy IS NULL OR (heartbeat_cull_strategy = ANY (ARRAY['DEACTIVATE_DEAD'::text, 'KEEP_DEAD'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_heartbeat_duration_min" CHECK (heartbeat_duration IS NULL OR heartbeat_duration >= 60);
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_heartbeat_resurrection_allowed" CHECK (heartbeat_resurrection_strategy IS NULL OR (heartbeat_resurrection_strategy = ANY (ARRAY['ALWAYS_REVIVE'::text, '15_MINUTE_REVIVE'::text, '10_MINUTE_REVIVE'::text, '5_MINUTE_REVIVE'::text, '2_MINUTE_REVIVE'::text, '1_MINUTE_REVIVE'::text, 'NO_REVIVE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_limits_nonnegative" CHECK ((max_machines IS NULL OR max_machines >= 0) AND (max_processes IS NULL OR max_processes >= 0) AND (max_users IS NULL OR max_users >= 0) AND (max_cores IS NULL OR max_cores >= 0) AND (max_memory IS NULL OR max_memory >= 0) AND (max_disk IS NULL OR max_disk >= 0) AND (max_uses IS NULL OR max_uses >= 0));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_machine_leasing_allowed" CHECK (machine_leasing_strategy IS NULL OR (machine_leasing_strategy = ANY (ARRAY['PER_LICENSE'::text, 'PER_USER'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_machine_matching_allowed" CHECK (machine_matching_strategy IS NULL OR (machine_matching_strategy = ANY (ARRAY['MATCH_ANY'::text, 'MATCH_TWO'::text, 'MATCH_MOST'::text, 'MATCH_ALL'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_machine_uniqueness_allowed" CHECK (machine_uniqueness_strategy IS NULL OR (machine_uniqueness_strategy = ANY (ARRAY['UNIQUE_PER_ACCOUNT'::text, 'UNIQUE_PER_PRODUCT'::text, 'UNIQUE_PER_POLICY'::text, 'UNIQUE_PER_LICENSE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_overage_allowed" CHECK (overage_strategy IS NULL OR (overage_strategy = ANY (ARRAY['ALWAYS_ALLOW_OVERAGE'::text, 'ALLOW_1_25X_OVERAGE'::text, 'ALLOW_1_5X_OVERAGE'::text, 'ALLOW_2X_OVERAGE'::text, 'NO_OVERAGE'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_process_leasing_allowed" CHECK (process_leasing_strategy IS NULL OR (process_leasing_strategy = ANY (ARRAY['PER_LICENSE'::text, 'PER_MACHINE'::text, 'PER_USER'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_renewal_basis_allowed" CHECK (renewal_basis IS NULL OR (renewal_basis = ANY (ARRAY['FROM_EXPIRY'::text, 'FROM_NOW'::text, 'FROM_NOW_IF_EXPIRED'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_scheme_allowed" CHECK (scheme IS NULL OR (scheme = ANY (ARRAY['LEGACY_ENCRYPT'::text, 'RSA_2048_PKCS1_ENCRYPT'::text, 'RSA_2048_PKCS1_SIGN'::text, 'RSA_2048_PKCS1_PSS_SIGN'::text, 'RSA_2048_JWT_RS256'::text, 'RSA_2048_PKCS1_SIGN_V2'::text, 'RSA_2048_PKCS1_PSS_SIGN_V2'::text, 'ED25519_SIGN'::text, 'ECDSA_P256_SIGN'::text])));
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_transfer_strategy_allowed" CHECK (transfer_strategy IS NULL OR (transfer_strategy = ANY (ARRAY['RESET_EXPIRY'::text, 'KEEP_EXPIRY'::text])));
ALTER TABLE "lic"."products" ADD CONSTRAINT "products_code_format" CHECK (code IS NULL OR code ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]*$'::text);
ALTER TABLE "lic"."products" ADD CONSTRAINT "products_distribution_strategy" CHECK (distribution_strategy IS NULL OR (distribution_strategy = ANY (ARRAY['OPEN'::text, 'SECRET'::text, 'CLOSED'::text])));
ALTER TABLE "lic"."tokens" ADD CONSTRAINT "tokens_activations_range" CHECK ((max_activations IS NULL OR max_activations >= 0) AND (max_deactivations IS NULL OR max_deactivations >= 0) AND activations >= 0 AND deactivations >= 0);
ALTER TABLE "lic"."tokens" ADD CONSTRAINT "tokens_bearer_type_allowed" CHECK (bearer_type IS NULL OR (bearer_type = ANY (ARRAY['User'::text, 'Product'::text, 'License'::text])));
ALTER TABLE "lic"."tokens" ADD CONSTRAINT "tokens_digest_nonempty" CHECK (length(btrim(digest)) > 0);
ALTER TABLE "lic"."users" ADD CONSTRAINT "users_email_format" CHECK (email IS NULL OR email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'::text);
ALTER TABLE "lic"."users" ADD CONSTRAINT "users_role_allowed" CHECK (role = ANY (ARRAY['user'::text, 'admin'::text, 'developer'::text, 'read_only'::text, 'sales_agent'::text, 'support_agent'::text]));
ALTER TABLE "lic"."webhook_endpoints" ADD CONSTRAINT "webhook_endpoints_api_version_format" CHECK (api_version IS NULL OR api_version ~ '^[0-9]+\.[0-9]+$'::text);
ALTER TABLE "lic"."webhook_endpoints" ADD CONSTRAINT "webhook_endpoints_signature_algorithm_allowed" CHECK (signature_algorithm = ANY (ARRAY['ed25519'::text, 'rsa-pss-sha256'::text, 'rsa-sha256'::text, 'ecdsa-p256'::text]));
ALTER TABLE "lic"."webhook_endpoints" ADD CONSTRAINT "webhook_endpoints_url_https" CHECK (url ~ '^https://'::text);
ALTER TABLE "lic"."webhook_events" ADD CONSTRAINT "webhook_events_attempts_nonnegative" CHECK (attempts >= 0);
ALTER TABLE "lic"."webhook_events" ADD CONSTRAINT "webhook_events_status_allowed" CHECK (status = ANY (ARRAY['DELIVERING'::text, 'DELIVERED'::text, 'FAILING'::text, 'FAILED'::text]));
CREATE INDEX "accounts_created_at_id_idx" ON "lic"."accounts" ("created_at" DESC, "id" DESC);
CREATE UNIQUE INDEX "accounts_slug_key" ON "lic"."accounts" ("slug");
CREATE UNIQUE INDEX "entitlements_account_code_key" ON "lic"."entitlements" ("account_id", "code");
CREATE INDEX "entitlements_created_at_id_idx" ON "lic"."entitlements" ("created_at" DESC, "id" DESC);
CREATE INDEX "group_owners_account_idx" ON "lic"."group_owners" ("account_id");
CREATE UNIQUE INDEX "group_owners_group_user_key" ON "lic"."group_owners" ("group_id", "user_id");
CREATE INDEX "group_owners_user_idx" ON "lic"."group_owners" ("user_id");
CREATE INDEX "groups_account_created_at_id_idx" ON "lic"."groups" ("account_id", "created_at" DESC, "id" DESC);
CREATE UNIQUE INDEX "keys_account_key_key" ON "lic"."keys" ("account_id", "key");
CREATE INDEX "keys_created_at_id_idx" ON "lic"."keys" ("created_at" DESC, "id" DESC);
CREATE INDEX "keys_policy_idx" ON "lic"."keys" ("policy_id");
CREATE INDEX "license_entitlements_entitlement_idx" ON "lic"."license_entitlements" ("entitlement_id");
CREATE UNIQUE INDEX "license_entitlements_ids_key" ON "lic"."license_entitlements" ("account_id", "license_id", "entitlement_id");
CREATE INDEX "license_entitlements_license_idx" ON "lic"."license_entitlements" ("license_id");
CREATE UNIQUE INDEX "license_users_ids_key" ON "lic"."license_users" ("account_id", "license_id", "user_id");
CREATE INDEX "license_users_license_idx" ON "lic"."license_users" ("license_id");
CREATE INDEX "license_users_user_idx" ON "lic"."license_users" ("user_id");
CREATE INDEX "licenses_account_created_at_id_idx" ON "lic"."licenses" ("account_id", "created_at" DESC, "id" DESC);
CREATE UNIQUE INDEX "licenses_account_key_key" ON "lic"."licenses" ("account_id", "key");
CREATE INDEX "licenses_expiry_idx" ON "lic"."licenses" ("expiry") WHERE (expiry IS NOT NULL);
CREATE INDEX "licenses_group_idx" ON "lic"."licenses" ("group_id");
CREATE INDEX "licenses_key_idx" ON "lic"."licenses" ("key");
CREATE INDEX "licenses_last_validated_idx" ON "lic"."licenses" ("last_validated_at");
CREATE INDEX "licenses_policy_idx" ON "lic"."licenses" ("policy_id");
CREATE INDEX "licenses_product_idx" ON "lic"."licenses" ("product_id");
CREATE INDEX "licenses_user_idx" ON "lic"."licenses" ("user_id");
CREATE INDEX "machine_components_account_created_at_id_idx" ON "lic"."machine_components" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "machine_components_fingerprint_idx" ON "lic"."machine_components" ("fingerprint");
CREATE UNIQUE INDEX "machine_components_machine_fingerprint_key" ON "lic"."machine_components" ("machine_id", "fingerprint");
CREATE INDEX "machine_processes_account_created_at_id_idx" ON "lic"."machine_processes" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "machine_processes_last_heartbeat_idx" ON "lic"."machine_processes" ("last_heartbeat_at");
CREATE UNIQUE INDEX "machine_processes_machine_pid_key" ON "lic"."machine_processes" ("machine_id", "pid");
CREATE INDEX "machines_account_created_at_id_idx" ON "lic"."machines" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "machines_fingerprint_idx" ON "lic"."machines" ("fingerprint");
CREATE INDEX "machines_last_heartbeat_idx" ON "lic"."machines" ("last_heartbeat_at");
CREATE UNIQUE INDEX "machines_license_fingerprint_key" ON "lic"."machines" ("license_id", "fingerprint");
CREATE INDEX "machines_license_idx" ON "lic"."machines" ("license_id");
CREATE INDEX "machines_owner_idx" ON "lic"."machines" ("owner_id");
CREATE INDEX "policies_account_created_at_id_idx" ON "lic"."policies" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "policies_product_id_idx" ON "lic"."policies" ("product_id");
CREATE INDEX "policies_require_heartbeat_idx" ON "lic"."policies" ("require_heartbeat") WHERE require_heartbeat;
CREATE INDEX "policy_entitlements_entitlement_idx" ON "lic"."policy_entitlements" ("entitlement_id");
CREATE UNIQUE INDEX "policy_entitlements_ids_key" ON "lic"."policy_entitlements" ("account_id", "policy_id", "entitlement_id");
CREATE INDEX "policy_entitlements_policy_idx" ON "lic"."policy_entitlements" ("policy_id");
CREATE UNIQUE INDEX "products_account_code_key" ON "lic"."products" ("account_id", "code");
CREATE INDEX "products_account_created_at_id_idx" ON "lic"."products" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "products_code_idx" ON "lic"."products" ("code");
CREATE UNIQUE INDEX "second_factors_secret_key" ON "lic"."second_factors" ("secret");
CREATE UNIQUE INDEX "second_factors_user_key" ON "lic"."second_factors" ("user_id");
CREATE INDEX "tokens_account_created_at_id_idx" ON "lic"."tokens" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "tokens_bearer_idx" ON "lic"."tokens" ("bearer_id", "bearer_type");
CREATE UNIQUE INDEX "tokens_digest_key" ON "lic"."tokens" ("digest");
CREATE INDEX "tokens_expiry_idx" ON "lic"."tokens" ("expiry") WHERE (expiry IS NOT NULL);
CREATE UNIQUE INDEX "users_account_email_key" ON "lic"."users" ("account_id", "email");
CREATE INDEX "users_created_at_id_idx" ON "lic"."users" ("created_at" DESC, "id" DESC);
CREATE INDEX "users_group_idx" ON "lic"."users" ("group_id");
CREATE INDEX "webhook_endpoints_account_created_at_id_idx" ON "lic"."webhook_endpoints" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "webhook_endpoints_product_idx" ON "lic"."webhook_endpoints" ("product_id");
CREATE INDEX "webhook_events_account_created_at_id_idx" ON "lic"."webhook_events" ("account_id", "created_at" DESC, "id" DESC);
CREATE INDEX "webhook_events_due_idx" ON "lic"."webhook_events" ("next_attempt_at") WHERE (status = ANY (ARRAY['DELIVERING'::text, 'FAILING'::text]));
CREATE INDEX "webhook_events_endpoint_idx" ON "lic"."webhook_events" ("webhook_endpoint_id");
CREATE INDEX "webhook_events_idempotency_idx" ON "lic"."webhook_events" ("idempotency_token");
ALTER TABLE "lic"."entitlements" ADD CONSTRAINT "entitlements_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."group_owners" ADD CONSTRAINT "group_owners_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."group_owners" ADD CONSTRAINT "group_owners_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "lic"."groups" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."group_owners" ADD CONSTRAINT "group_owners_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "lic"."users" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."groups" ADD CONSTRAINT "groups_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."keys" ADD CONSTRAINT "keys_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."keys" ADD CONSTRAINT "keys_policy_id_fkey" FOREIGN KEY ("policy_id") REFERENCES "lic"."policies" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_entitlements" ADD CONSTRAINT "license_entitlements_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_entitlements" ADD CONSTRAINT "license_entitlements_entitlement_id_fkey" FOREIGN KEY ("entitlement_id") REFERENCES "lic"."entitlements" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_entitlements" ADD CONSTRAINT "license_entitlements_license_id_fkey" FOREIGN KEY ("license_id") REFERENCES "lic"."licenses" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_users" ADD CONSTRAINT "license_users_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_users" ADD CONSTRAINT "license_users_license_id_fkey" FOREIGN KEY ("license_id") REFERENCES "lic"."licenses" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."license_users" ADD CONSTRAINT "license_users_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "lic"."users" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "lic"."groups" ("id") ON DELETE SET NULL;
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_policy_id_fkey" FOREIGN KEY ("policy_id") REFERENCES "lic"."policies" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_product_id_fkey" FOREIGN KEY ("product_id") REFERENCES "lic"."products" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."licenses" ADD CONSTRAINT "licenses_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "lic"."users" ("id") ON DELETE SET NULL;
ALTER TABLE "lic"."machine_components" ADD CONSTRAINT "machine_components_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_components" ADD CONSTRAINT "machine_components_machine_id_fkey" FOREIGN KEY ("machine_id") REFERENCES "lic"."machines" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_processes" ADD CONSTRAINT "machine_processes_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_processes" ADD CONSTRAINT "machine_processes_machine_id_fkey" FOREIGN KEY ("machine_id") REFERENCES "lic"."machines" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "lic"."groups" ("id") ON DELETE SET NULL;
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_license_id_fkey" FOREIGN KEY ("license_id") REFERENCES "lic"."licenses" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_owner_id_fkey" FOREIGN KEY ("owner_id") REFERENCES "lic"."users" ("id") ON DELETE SET NULL;
ALTER TABLE "lic"."machines" ADD CONSTRAINT "machines_policy_id_fkey" FOREIGN KEY ("policy_id") REFERENCES "lic"."policies" ("id");
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."policies" ADD CONSTRAINT "policies_product_id_fkey" FOREIGN KEY ("product_id") REFERENCES "lic"."products" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."policy_entitlements" ADD CONSTRAINT "policy_entitlements_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."policy_entitlements" ADD CONSTRAINT "policy_entitlements_entitlement_id_fkey" FOREIGN KEY ("entitlement_id") REFERENCES "lic"."entitlements" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."policy_entitlements" ADD CONSTRAINT "policy_entitlements_policy_id_fkey" FOREIGN KEY ("policy_id") REFERENCES "lic"."policies" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."products" ADD CONSTRAINT "products_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."second_factors" ADD CONSTRAINT "second_factors_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."second_factors" ADD CONSTRAINT "second_factors_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "lic"."users" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."tokens" ADD CONSTRAINT "tokens_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."users" ADD CONSTRAINT "users_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."users" ADD CONSTRAINT "users_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "lic"."groups" ("id") ON DELETE SET NULL;
ALTER TABLE "lic"."webhook_endpoints" ADD CONSTRAINT "webhook_endpoints_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."webhook_endpoints" ADD CONSTRAINT "webhook_endpoints_product_id_fkey" FOREIGN KEY ("product_id") REFERENCES "lic"."products" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."webhook_events" ADD CONSTRAINT "webhook_events_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."webhook_events" ADD CONSTRAINT "webhook_events_webhook_endpoint_id_fkey" FOREIGN KEY ("webhook_endpoint_id") REFERENCES "lic"."webhook_endpoints" ("id") ON DELETE SET NULL;

-- sqld:down
ALTER TABLE "lic"."webhook_events" DROP CONSTRAINT "webhook_events_webhook_endpoint_id_fkey";
ALTER TABLE "lic"."webhook_events" DROP CONSTRAINT "webhook_events_account_id_fkey";
ALTER TABLE "lic"."webhook_endpoints" DROP CONSTRAINT "webhook_endpoints_product_id_fkey";
ALTER TABLE "lic"."webhook_endpoints" DROP CONSTRAINT "webhook_endpoints_account_id_fkey";
ALTER TABLE "lic"."users" DROP CONSTRAINT "users_group_id_fkey";
ALTER TABLE "lic"."users" DROP CONSTRAINT "users_account_id_fkey";
ALTER TABLE "lic"."tokens" DROP CONSTRAINT "tokens_account_id_fkey";
ALTER TABLE "lic"."second_factors" DROP CONSTRAINT "second_factors_user_id_fkey";
ALTER TABLE "lic"."second_factors" DROP CONSTRAINT "second_factors_account_id_fkey";
ALTER TABLE "lic"."products" DROP CONSTRAINT "products_account_id_fkey";
ALTER TABLE "lic"."policy_entitlements" DROP CONSTRAINT "policy_entitlements_policy_id_fkey";
ALTER TABLE "lic"."policy_entitlements" DROP CONSTRAINT "policy_entitlements_entitlement_id_fkey";
ALTER TABLE "lic"."policy_entitlements" DROP CONSTRAINT "policy_entitlements_account_id_fkey";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_product_id_fkey";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_account_id_fkey";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_policy_id_fkey";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_owner_id_fkey";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_license_id_fkey";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_group_id_fkey";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_account_id_fkey";
ALTER TABLE "lic"."machine_processes" DROP CONSTRAINT "machine_processes_machine_id_fkey";
ALTER TABLE "lic"."machine_processes" DROP CONSTRAINT "machine_processes_account_id_fkey";
ALTER TABLE "lic"."machine_components" DROP CONSTRAINT "machine_components_machine_id_fkey";
ALTER TABLE "lic"."machine_components" DROP CONSTRAINT "machine_components_account_id_fkey";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_user_id_fkey";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_product_id_fkey";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_policy_id_fkey";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_group_id_fkey";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_account_id_fkey";
ALTER TABLE "lic"."license_users" DROP CONSTRAINT "license_users_user_id_fkey";
ALTER TABLE "lic"."license_users" DROP CONSTRAINT "license_users_license_id_fkey";
ALTER TABLE "lic"."license_users" DROP CONSTRAINT "license_users_account_id_fkey";
ALTER TABLE "lic"."license_entitlements" DROP CONSTRAINT "license_entitlements_license_id_fkey";
ALTER TABLE "lic"."license_entitlements" DROP CONSTRAINT "license_entitlements_entitlement_id_fkey";
ALTER TABLE "lic"."license_entitlements" DROP CONSTRAINT "license_entitlements_account_id_fkey";
ALTER TABLE "lic"."keys" DROP CONSTRAINT "keys_policy_id_fkey";
ALTER TABLE "lic"."keys" DROP CONSTRAINT "keys_account_id_fkey";
ALTER TABLE "lic"."groups" DROP CONSTRAINT "groups_account_id_fkey";
ALTER TABLE "lic"."group_owners" DROP CONSTRAINT "group_owners_user_id_fkey";
ALTER TABLE "lic"."group_owners" DROP CONSTRAINT "group_owners_group_id_fkey";
ALTER TABLE "lic"."group_owners" DROP CONSTRAINT "group_owners_account_id_fkey";
ALTER TABLE "lic"."entitlements" DROP CONSTRAINT "entitlements_account_id_fkey";
DROP INDEX "lic"."webhook_events_idempotency_idx";
DROP INDEX "lic"."webhook_events_endpoint_idx";
DROP INDEX "lic"."webhook_events_due_idx";
DROP INDEX "lic"."webhook_events_account_created_at_id_idx";
DROP INDEX "lic"."webhook_endpoints_product_idx";
DROP INDEX "lic"."webhook_endpoints_account_created_at_id_idx";
DROP INDEX "lic"."users_group_idx";
DROP INDEX "lic"."users_created_at_id_idx";
DROP INDEX "lic"."users_account_email_key";
DROP INDEX "lic"."tokens_expiry_idx";
DROP INDEX "lic"."tokens_digest_key";
DROP INDEX "lic"."tokens_bearer_idx";
DROP INDEX "lic"."tokens_account_created_at_id_idx";
DROP INDEX "lic"."second_factors_user_key";
DROP INDEX "lic"."second_factors_secret_key";
DROP INDEX "lic"."products_code_idx";
DROP INDEX "lic"."products_account_created_at_id_idx";
DROP INDEX "lic"."products_account_code_key";
DROP INDEX "lic"."policy_entitlements_policy_idx";
DROP INDEX "lic"."policy_entitlements_ids_key";
DROP INDEX "lic"."policy_entitlements_entitlement_idx";
DROP INDEX "lic"."policies_require_heartbeat_idx";
DROP INDEX "lic"."policies_product_id_idx";
DROP INDEX "lic"."policies_account_created_at_id_idx";
DROP INDEX "lic"."machines_owner_idx";
DROP INDEX "lic"."machines_license_idx";
DROP INDEX "lic"."machines_license_fingerprint_key";
DROP INDEX "lic"."machines_last_heartbeat_idx";
DROP INDEX "lic"."machines_fingerprint_idx";
DROP INDEX "lic"."machines_account_created_at_id_idx";
DROP INDEX "lic"."machine_processes_machine_pid_key";
DROP INDEX "lic"."machine_processes_last_heartbeat_idx";
DROP INDEX "lic"."machine_processes_account_created_at_id_idx";
DROP INDEX "lic"."machine_components_machine_fingerprint_key";
DROP INDEX "lic"."machine_components_fingerprint_idx";
DROP INDEX "lic"."machine_components_account_created_at_id_idx";
DROP INDEX "lic"."licenses_user_idx";
DROP INDEX "lic"."licenses_product_idx";
DROP INDEX "lic"."licenses_policy_idx";
DROP INDEX "lic"."licenses_last_validated_idx";
DROP INDEX "lic"."licenses_key_idx";
DROP INDEX "lic"."licenses_group_idx";
DROP INDEX "lic"."licenses_expiry_idx";
DROP INDEX "lic"."licenses_account_key_key";
DROP INDEX "lic"."licenses_account_created_at_id_idx";
DROP INDEX "lic"."license_users_user_idx";
DROP INDEX "lic"."license_users_license_idx";
DROP INDEX "lic"."license_users_ids_key";
DROP INDEX "lic"."license_entitlements_license_idx";
DROP INDEX "lic"."license_entitlements_ids_key";
DROP INDEX "lic"."license_entitlements_entitlement_idx";
DROP INDEX "lic"."keys_policy_idx";
DROP INDEX "lic"."keys_created_at_id_idx";
DROP INDEX "lic"."keys_account_key_key";
DROP INDEX "lic"."groups_account_created_at_id_idx";
DROP INDEX "lic"."group_owners_user_idx";
DROP INDEX "lic"."group_owners_group_user_key";
DROP INDEX "lic"."group_owners_account_idx";
DROP INDEX "lic"."entitlements_created_at_id_idx";
DROP INDEX "lic"."entitlements_account_code_key";
DROP INDEX "lic"."accounts_slug_key";
DROP INDEX "lic"."accounts_created_at_id_idx";
ALTER TABLE "lic"."webhook_events" DROP CONSTRAINT "webhook_events_status_allowed";
ALTER TABLE "lic"."webhook_events" DROP CONSTRAINT "webhook_events_attempts_nonnegative";
ALTER TABLE "lic"."webhook_endpoints" DROP CONSTRAINT "webhook_endpoints_url_https";
ALTER TABLE "lic"."webhook_endpoints" DROP CONSTRAINT "webhook_endpoints_signature_algorithm_allowed";
ALTER TABLE "lic"."webhook_endpoints" DROP CONSTRAINT "webhook_endpoints_api_version_format";
ALTER TABLE "lic"."users" DROP CONSTRAINT "users_role_allowed";
ALTER TABLE "lic"."users" DROP CONSTRAINT "users_email_format";
ALTER TABLE "lic"."tokens" DROP CONSTRAINT "tokens_digest_nonempty";
ALTER TABLE "lic"."tokens" DROP CONSTRAINT "tokens_bearer_type_allowed";
ALTER TABLE "lic"."tokens" DROP CONSTRAINT "tokens_activations_range";
ALTER TABLE "lic"."products" DROP CONSTRAINT "products_distribution_strategy";
ALTER TABLE "lic"."products" DROP CONSTRAINT "products_code_format";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_transfer_strategy_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_scheme_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_renewal_basis_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_process_leasing_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_overage_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_machine_uniqueness_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_machine_matching_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_machine_leasing_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_limits_nonnegative";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_heartbeat_resurrection_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_heartbeat_duration_min";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_heartbeat_cull_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_heartbeat_basis_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_expiration_strategy_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_expiration_basis_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_duration_positive";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_component_uniqueness_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_component_matching_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_check_in_interval_allowed";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_check_in_count_range";
ALTER TABLE "lic"."policies" DROP CONSTRAINT "policies_authentication_strategy_allowed";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_resources_nonnegative";
ALTER TABLE "lic"."machines" DROP CONSTRAINT "machines_fingerprint_nonempty";
ALTER TABLE "lic"."machine_processes" DROP CONSTRAINT "machine_processes_pid_nonempty";
ALTER TABLE "lic"."machine_components" DROP CONSTRAINT "machine_components_names_nonempty";
ALTER TABLE "lic"."machine_components" DROP CONSTRAINT "machine_components_fingerprint_nonempty";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_uses_nonnegative";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_limits_nonnegative";
ALTER TABLE "lic"."licenses" DROP CONSTRAINT "licenses_counts_nonnegative";
ALTER TABLE "lic"."keys" DROP CONSTRAINT "keys_nonempty";
ALTER TABLE "lic"."groups" DROP CONSTRAINT "groups_limits_nonnegative";
ALTER TABLE "lic"."entitlements" DROP CONSTRAINT "entitlements_code_format";
ALTER TABLE "lic"."accounts" DROP CONSTRAINT "accounts_slug_format";
ALTER TABLE "lic"."accounts" DROP CONSTRAINT "accounts_api_version_format";
DROP TABLE "lic"."webhook_events";
DROP TABLE "lic"."webhook_endpoints";
DROP TABLE "lic"."users";
DROP TABLE "lic"."tokens";
DROP TABLE "lic"."second_factors";
DROP TABLE "lic"."products";
DROP TABLE "lic"."policy_entitlements";
DROP TABLE "lic"."policies";
DROP TABLE "lic"."machines";
DROP TABLE "lic"."machine_processes";
DROP TABLE "lic"."machine_components";
DROP TABLE "lic"."licenses";
DROP TABLE "lic"."license_users";
DROP TABLE "lic"."license_entitlements";
DROP TABLE "lic"."keys";
DROP TABLE "lic"."groups";
DROP TABLE "lic"."group_owners";
DROP TABLE "lic"."entitlements";
DROP TABLE "lic"."accounts";
