-- Second factors (internal/app/secondfactors, internal/app/authn).

-- name: GetSecondFactorByID :one
SELECT id, account_id, user_id, secret, enabled, last_verified_at, created_at, updated_at
FROM lic.second_factors
WHERE account_id = @account_id AND user_id = @user_id AND id = @id
FOR UPDATE;

-- name: GetSecondFactorAny :one
SELECT id, account_id, user_id, secret, enabled, last_verified_at, created_at, updated_at
FROM lic.second_factors
WHERE user_id = @user_id
LIMIT 1 FOR UPDATE;

-- name: InsertSecondFactor :exec
INSERT INTO lic.second_factors (id, account_id, user_id, secret)
VALUES (@id, @account_id, @user_id, @secret);

-- name: UpdateSecondFactor :exec
UPDATE lic.second_factors
SET enabled = @enabled, last_verified_at = @last_verified_at, updated_at = now()
WHERE account_id = @account_id AND user_id = @user_id AND id = @id;

-- name: DeleteSecondFactor :exec
DELETE FROM lic.second_factors
WHERE account_id = @account_id AND user_id = @user_id AND id = @id;

-- name: ListSecondFactors :many
SELECT id, account_id, user_id, secret, enabled, last_verified_at, created_at, updated_at
FROM lic.second_factors
WHERE account_id = @account_id AND user_id = @user_id
ORDER BY created_at DESC, id DESC
LIMIT @limit;

-- name: GetEnabledSecondFactorSecret :one
SELECT secret
FROM lic.second_factors
WHERE user_id = @user_id AND enabled
LIMIT 1;
