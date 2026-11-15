-- sqld:up
CREATE SEQUENCE "lic"."outbox_messages_seq_seq" AS int8 INCREMENT BY 1 MINVALUE 1 MAXVALUE 9223372036854775807 START WITH 1 CACHE 1;
CREATE TABLE "lic"."outbox_tombstones" (
  "id" uuid NOT NULL,
  "status" text NOT NULL,
  "finished_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
ALTER TABLE "lic"."outbox_messages" ADD COLUMN "seq" int8 NOT NULL DEFAULT nextval('lic.outbox_messages_seq_seq'::regclass);
ALTER TABLE "lic"."outbox_tombstones" ADD CONSTRAINT "outbox_tombstones_status_check" CHECK (status = ANY (ARRAY['dead'::text, 'discarded'::text]));
CREATE INDEX "outbox_messages_partition_seq_idx" ON "lic"."outbox_messages" ("partition_key", "created_at", "seq") WHERE (partition_key IS NOT NULL);
DROP INDEX "lic"."outbox_messages_partition_idx";
CREATE INDEX "outbox_tombstones_finished_idx" ON "lic"."outbox_tombstones" ("finished_at");

-- sqld:down
DROP INDEX "lic"."outbox_tombstones_finished_idx";
CREATE INDEX "outbox_messages_partition_idx" ON "lic"."outbox_messages" ("partition_key", "created_at") WHERE (partition_key IS NOT NULL);
DROP INDEX "lic"."outbox_messages_partition_seq_idx";
ALTER TABLE "lic"."outbox_tombstones" DROP CONSTRAINT "outbox_tombstones_status_check";
ALTER TABLE "lic"."outbox_messages" DROP COLUMN "seq";
DROP TABLE "lic"."outbox_tombstones";
DROP SEQUENCE "lic"."outbox_messages_seq_seq";
