-- name: LockAutomationActivity :exec
SELECT pg_advisory_xact_lock(hashtextextended(@account_id::text || ':lic:activity:' || @execution_key::text, 0));

-- name: GetAutomationActivityResult :one
SELECT request_hash, result FROM lic.automation_activity_results
WHERE account_id = @account_id AND execution_key = @execution_key;

-- name: SaveAutomationActivityResult :exec
INSERT INTO lic.automation_activity_results (account_id, execution_key, request_hash, result)
VALUES (@account_id, @execution_key, @request_hash, @result);

-- Automation integration tests read only the current account's committed work.
-- name: ListAutomationMessages :many
SELECT id, topic, message_type, payload, status, attempts
FROM lic.outbox_messages
WHERE partition_key = @account_id::text AND topic IN ('lic.events', 'lic.deliver')
ORDER BY created_at, id;
