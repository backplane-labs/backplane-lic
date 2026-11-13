-- Machine usage journals (internal/app/machines): the head serializes
-- reports of one machine; events and acks are append-only.

-- name: UsageHeadForUpdate :many
SELECT epoch, seq, hash FROM lic.machine_usage_heads WHERE machine_id = @machine_id FOR UPDATE;

-- name: UpsertUsageHead :exec
INSERT INTO lic.machine_usage_heads (machine_id, account_id, epoch, seq, hash, updated_at)
VALUES (@machine_id, @account_id, @epoch, @seq, @hash, @updated_at)
ON CONFLICT (machine_id) DO UPDATE SET epoch = EXCLUDED.epoch, seq = EXCLUDED.seq, hash = EXCLUDED.hash,
  updated_at = EXCLUDED.updated_at;

-- name: InsertUsageEvent :exec
INSERT INTO lic.machine_usage_events (machine_id, epoch, seq, event, hash, created_at)
VALUES (@machine_id, @epoch, @seq, @event, @hash, @created_at);

-- name: InsertUsageAck :exec
INSERT INTO lic.machine_usage_acks (machine_id, epoch, seq, hash, counters, build, created_at)
VALUES (@machine_id, @epoch, @seq, @hash, @counters, @build, @created_at);

-- name: UsageHead :many
SELECT epoch, seq, hash FROM lic.machine_usage_heads WHERE machine_id = @machine_id;

-- name: UsageEvents :many
SELECT seq, event, hash FROM lic.machine_usage_events
WHERE machine_id = @machine_id AND epoch = @epoch AND seq > @after
ORDER BY seq
LIMIT @lim;

-- name: UsageAcks :many
SELECT epoch, seq, hash FROM lic.machine_usage_acks
WHERE machine_id = @machine_id AND epoch = @epoch AND seq > @after
ORDER BY seq
LIMIT @lim;
