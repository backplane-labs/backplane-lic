-- sqld:up
CREATE TABLE "lic"."machine_usage_acks" (
  "machine_id" uuid NOT NULL,
  "epoch" int8 NOT NULL,
  "seq" int8 NOT NULL,
  "hash" text NOT NULL,
  "counters" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "build" text,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("machine_id", "epoch", "seq")
);
CREATE TABLE "lic"."machine_usage_events" (
  "machine_id" uuid NOT NULL,
  "epoch" int8 NOT NULL,
  "seq" int8 NOT NULL,
  "event" bytea NOT NULL,
  "hash" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("machine_id", "epoch", "seq")
);
CREATE TABLE "lic"."machine_usage_heads" (
  "machine_id" uuid NOT NULL,
  "account_id" uuid NOT NULL,
  "epoch" int8 NOT NULL,
  "seq" int8 NOT NULL,
  "hash" text NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("machine_id")
);
ALTER TABLE "lic"."machine_usage_acks" ADD CONSTRAINT "machine_usage_acks_machine_id_fkey" FOREIGN KEY ("machine_id") REFERENCES "lic"."machines" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_usage_events" ADD CONSTRAINT "machine_usage_events_machine_id_fkey" FOREIGN KEY ("machine_id") REFERENCES "lic"."machines" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_usage_heads" ADD CONSTRAINT "machine_usage_heads_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "lic"."accounts" ("id") ON DELETE CASCADE;
ALTER TABLE "lic"."machine_usage_heads" ADD CONSTRAINT "machine_usage_heads_machine_id_fkey" FOREIGN KEY ("machine_id") REFERENCES "lic"."machines" ("id") ON DELETE CASCADE;

-- sqld:down
ALTER TABLE "lic"."machine_usage_heads" DROP CONSTRAINT "machine_usage_heads_machine_id_fkey";
ALTER TABLE "lic"."machine_usage_heads" DROP CONSTRAINT "machine_usage_heads_account_id_fkey";
ALTER TABLE "lic"."machine_usage_events" DROP CONSTRAINT "machine_usage_events_machine_id_fkey";
ALTER TABLE "lic"."machine_usage_acks" DROP CONSTRAINT "machine_usage_acks_machine_id_fkey";
DROP TABLE "lic"."machine_usage_heads";
DROP TABLE "lic"."machine_usage_events";
DROP TABLE "lic"."machine_usage_acks";
