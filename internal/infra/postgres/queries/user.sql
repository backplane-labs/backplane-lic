-- Users (internal/app/users, internal/app/authn).

-- name: GetUserByEmail :one
SELECT id, account_id, first_name, last_name, email, password_digest, role,
  permissions, group_id, metadata, banned_at, created_at, updated_at
FROM lic.users
WHERE account_id = @account_id AND lower(email) = lower(@email);

-- name: GetUserByID :one
SELECT id, account_id, first_name, last_name, email, password_digest, role,
  permissions, group_id, metadata, banned_at, created_at, updated_at
FROM lic.users
WHERE account_id = @account_id AND id = @id;

-- name: InsertUser :exec
INSERT INTO lic.users (
  id, account_id, first_name, last_name, email, password_digest, role,
  permissions, metadata
) VALUES (
  @id, @account_id, @first_name, @last_name, @email, @password_digest, @role,
  @permissions, @metadata
);

-- name: UserHasActiveLicense :one
SELECT EXISTS (
  SELECT 1
  FROM lic.licenses l
  WHERE l.account_id = @account_id
    AND (
      l.user_id = @id
      OR EXISTS (
        SELECT 1 FROM lic.license_users lu
        WHERE lu.license_id = l.id AND lu.user_id = @id
      )
    )
    AND GREATEST(
      l.created_at,
      COALESCE(l.last_validated_at, l.created_at),
      COALESCE(l.last_check_in_at, l.created_at),
      COALESCE(l.last_check_out_at, l.created_at)
    ) >= @cutoff
) AS active;

-- name: GetEnabledSecondFactorSecret :one
SELECT secret
FROM lic.second_factors
WHERE user_id = @user_id AND enabled
LIMIT 1;
