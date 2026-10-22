-- Account-scoped operator overview. One statement gives a consistent snapshot.
-- name: ConsoleCounts :one
SELECT
  (SELECT count(*) FROM lic.products WHERE account_id = @account_id)::bigint AS products,
  (SELECT count(*) FROM lic.policies WHERE account_id = @account_id)::bigint AS policies,
  (SELECT count(*) FROM lic.licenses WHERE account_id = @account_id)::bigint AS licenses,
  (SELECT count(*) FROM lic.users WHERE account_id = @account_id)::bigint AS users,
  (SELECT count(*) FROM lic.groups WHERE account_id = @account_id)::bigint AS groups,
  (SELECT count(*) FROM lic.entitlements WHERE account_id = @account_id)::bigint AS entitlements,
  (SELECT count(*) FROM lic.tokens WHERE account_id = @account_id)::bigint AS tokens,
  (SELECT count(*) FROM lic.webhook_endpoints WHERE account_id = @account_id)::bigint AS webhook_endpoints,
  (SELECT count(*) FROM lic.webhook_events WHERE account_id = @account_id)::bigint AS webhook_events;
