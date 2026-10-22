-- Periodic jobs (internal/app/jobs): the prune work Keygen runs on cron,
-- plus the advisory lock that lets one replica run a tick at a time.

-- name: TryJobsAdvisoryLock :one
SELECT pg_try_advisory_xact_lock(hashtext('lic:jobs'))::boolean AS locked;

-- name: PruneWebhookEventsBefore :execrows
DELETE FROM lic.webhook_events
WHERE created_at < @cutoff
  AND (@account_id::uuid IS NULL OR account_id = @account_id);

-- name: PruneTokensExpiredBefore :execrows
DELETE FROM lic.tokens
WHERE expiry IS NOT NULL AND expiry < @cutoff;
