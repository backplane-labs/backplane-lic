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
