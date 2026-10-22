-- Users (internal/app/users, internal/app/authn, internal/app/passwords).

-- name: GetUserByEmail :one
SELECT id, account_id, first_name, last_name, email, password_digest, role,
  permissions, group_id, metadata, password_reset_token, password_reset_sent_at,
  banned_at, created_at, updated_at
FROM lic.users
WHERE account_id = @account_id AND lower(email) = lower(@email);

-- name: GetUserByID :one
SELECT id, account_id, first_name, last_name, email, password_digest, role,
  permissions, group_id, metadata, password_reset_token, password_reset_sent_at,
  banned_at, created_at, updated_at
FROM lic.users
WHERE account_id = @account_id AND id = @id;

-- name: InsertUser :exec
INSERT INTO lic.users (
  id, account_id, first_name, last_name, email, password_digest, role,
  permissions, group_id, metadata
) VALUES (
  @id, @account_id, @first_name, @last_name, @email, @password_digest, @role,
  @permissions, @group_id, @metadata
);

-- name: UpdateUserColumns :exec
UPDATE lic.users SET
 first_name = CASE WHEN @first_name_set THEN @first_name ELSE first_name END,
 last_name = CASE WHEN @last_name_set THEN @last_name ELSE last_name END,
 email = CASE WHEN @email_set THEN @email ELSE email END,
 password_digest = CASE WHEN @password_set THEN @password_digest ELSE password_digest END,
 role = CASE WHEN @role_set THEN @role ELSE role END,
 group_id = CASE WHEN @group_set THEN @group_id ELSE group_id END,
 metadata = CASE WHEN @metadata_set THEN @metadata ELSE metadata END,
 updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteUser :exec
DELETE FROM lic.users WHERE account_id = @account_id AND id = @id;

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

-- name: CountUsers :one
SELECT count(*)::int AS total FROM lic.users u
WHERE u.account_id = @account_id
  AND (sqlc.narg(filter_ids)::uuid[] IS NULL OR u.id = ANY(sqlc.narg(filter_ids)::uuid[]))
  AND (@roles::text[] IS NULL OR u.role = ANY(@roles::text[]))
  AND (@group_id::uuid IS NULL OR u.group_id = @group_id)
  AND (
    @product_id::uuid IS NULL
    OR EXISTS (
      SELECT 1 FROM lic.licenses l
      WHERE l.account_id = u.account_id AND l.user_id = u.id AND l.product_id = @product_id
    )
    OR EXISTS (
      SELECT 1
      FROM lic.license_users lu
      JOIN lic.licenses l ON l.id = lu.license_id
      WHERE lu.user_id = u.id AND l.product_id = @product_id
    )
  )
  AND (
    @license_id::uuid IS NULL
    OR EXISTS (SELECT 1 FROM lic.licenses l WHERE l.id = @license_id AND l.user_id = u.id)
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = @license_id AND lu.user_id = u.id
    )
  )
  AND (
    @status::text IS NULL
    OR CASE
      WHEN u.banned_at IS NOT NULL THEN 'BANNED'
      WHEN u.created_at >= @cutoff
        OR EXISTS (
          SELECT 1
          FROM lic.licenses l
          WHERE l.account_id = u.account_id
            AND (l.user_id = u.id OR EXISTS (
              SELECT 1 FROM lic.license_users lu
              WHERE lu.license_id = l.id AND lu.user_id = u.id
            ))
            AND GREATEST(
              l.created_at,
              COALESCE(l.last_validated_at, l.created_at),
              COALESCE(l.last_check_in_at, l.created_at),
              COALESCE(l.last_check_out_at, l.created_at)
            ) >= @cutoff
        )
      THEN 'ACTIVE'
      ELSE 'INACTIVE'
    END = @status
  )
  AND (
    @assigned::boolean IS NULL
    OR EXISTS (
      SELECT 1 FROM lic.licenses l WHERE l.account_id = u.account_id AND l.user_id = u.id
    ) = @assigned
  )
  AND (@metadata::jsonb IS NULL OR u.metadata @> @metadata)
  AND (
    @scope_kind::text IS NULL
    OR (@scope_kind::text = 'none' AND false)
    OR (@scope_kind::text = 'product' AND u.role = 'user')
    OR (
      @scope_kind::text = 'user'
      AND (
        u.id = @scope_id
        OR EXISTS (
          SELECT 1
          FROM lic.licenses l
          WHERE l.account_id = u.account_id
            AND (
              (l.user_id = @scope_id AND EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = u.id
              ))
              OR (l.user_id = u.id AND EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = @scope_id
              ))
            )
        )
      )
    )
    OR (
      @scope_kind::text = 'license'
      AND (
        EXISTS (SELECT 1 FROM lic.licenses l WHERE l.id = @scope_id AND l.user_id = u.id)
        OR EXISTS (
          SELECT 1 FROM lic.license_users lu
          WHERE lu.license_id = @scope_id AND lu.user_id = u.id
        )
      )
    )
  )
;

-- name: ListUsers :many
SELECT
  u.id, u.account_id, u.first_name, u.last_name, u.email, u.password_digest,
  u.role, u.permissions, u.group_id, u.metadata, u.banned_at, u.created_at, u.updated_at,
  CASE
    WHEN u.banned_at IS NOT NULL THEN 'BANNED'
    WHEN u.created_at >= @cutoff
      OR EXISTS (
        SELECT 1
        FROM lic.licenses l
        WHERE l.account_id = u.account_id
          AND (l.user_id = u.id OR EXISTS (
            SELECT 1 FROM lic.license_users lu
            WHERE lu.license_id = l.id AND lu.user_id = u.id
          ))
          AND GREATEST(
            l.created_at,
            COALESCE(l.last_validated_at, l.created_at),
            COALESCE(l.last_check_in_at, l.created_at),
            COALESCE(l.last_check_out_at, l.created_at)
          ) >= @cutoff
      )
    THEN 'ACTIVE'
    ELSE 'INACTIVE'
  END AS status
FROM lic.users u
WHERE u.account_id = @account_id
  AND (sqlc.narg(filter_ids)::uuid[] IS NULL OR u.id = ANY(sqlc.narg(filter_ids)::uuid[]))
  AND (@roles::text[] IS NULL OR u.role = ANY(@roles::text[]))
  AND (@group_id::uuid IS NULL OR u.group_id = @group_id)
  AND (
    @product_id::uuid IS NULL
    OR EXISTS (
      SELECT 1 FROM lic.licenses l
      WHERE l.account_id = u.account_id AND l.user_id = u.id AND l.product_id = @product_id
    )
    OR EXISTS (
      SELECT 1
      FROM lic.license_users lu
      JOIN lic.licenses l ON l.id = lu.license_id
      WHERE lu.user_id = u.id AND l.product_id = @product_id
    )
  )
  AND (
    @license_id::uuid IS NULL
    OR EXISTS (SELECT 1 FROM lic.licenses l WHERE l.id = @license_id AND l.user_id = u.id)
    OR EXISTS (
      SELECT 1 FROM lic.license_users lu
      WHERE lu.license_id = @license_id AND lu.user_id = u.id
    )
  )
  AND (
    @status::text IS NULL
    OR CASE
      WHEN u.banned_at IS NOT NULL THEN 'BANNED'
      WHEN u.created_at >= @cutoff
        OR EXISTS (
          SELECT 1
          FROM lic.licenses l
          WHERE l.account_id = u.account_id
            AND (l.user_id = u.id OR EXISTS (
              SELECT 1 FROM lic.license_users lu
              WHERE lu.license_id = l.id AND lu.user_id = u.id
            ))
            AND GREATEST(
              l.created_at,
              COALESCE(l.last_validated_at, l.created_at),
              COALESCE(l.last_check_in_at, l.created_at),
              COALESCE(l.last_check_out_at, l.created_at)
            ) >= @cutoff
        )
      THEN 'ACTIVE'
      ELSE 'INACTIVE'
    END = @status
  )
  AND (
    @assigned::boolean IS NULL
    OR EXISTS (
      SELECT 1 FROM lic.licenses l WHERE l.account_id = u.account_id AND l.user_id = u.id
    ) = @assigned
  )
  AND (@metadata::jsonb IS NULL OR u.metadata @> @metadata)
  AND (
    @scope_kind::text IS NULL
    OR (@scope_kind::text = 'none' AND false)
    OR (@scope_kind::text = 'product' AND u.role = 'user')
    OR (
      @scope_kind::text = 'user'
      AND (
        u.id = @scope_id
        OR EXISTS (
          SELECT 1
          FROM lic.licenses l
          WHERE l.account_id = u.account_id
            AND (
              (l.user_id = @scope_id AND EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = u.id
              ))
              OR (l.user_id = u.id AND EXISTS (
                SELECT 1 FROM lic.license_users lu
                WHERE lu.license_id = l.id AND lu.user_id = @scope_id
              ))
            )
        )
      )
    )
    OR (
      @scope_kind::text = 'license'
      AND (
        EXISTS (SELECT 1 FROM lic.licenses l WHERE l.id = @scope_id AND l.user_id = u.id)
        OR EXISTS (
          SELECT 1 FROM lic.license_users lu
          WHERE lu.license_id = @scope_id AND lu.user_id = u.id
        )
      )
    )
  )
  AND (
    @cursor::uuid IS NULL
    OR CASE
      WHEN @asc::boolean THEN
        (u.created_at, u.id) > (SELECT c.created_at, c.id FROM lic.users c WHERE c.id = @cursor)
      ELSE
        (u.created_at, u.id) < (SELECT c.created_at, c.id FROM lic.users c WHERE c.id = @cursor)
    END
  )
ORDER BY
  CASE WHEN @asc::boolean THEN u.created_at END ASC,
  CASE WHEN @asc::boolean THEN u.id END ASC,
  u.created_at DESC,
  u.id DESC
LIMIT @limit
OFFSET @offset;

-- name: CountAdmins :one
SELECT count(*)::int AS total FROM lic.users
WHERE account_id = @account_id AND role = 'admin';

-- name: UpdateUserPassword :exec
UPDATE lic.users SET
  password_digest = @password_digest,
  password_reset_token = NULL,
  password_reset_sent_at = NULL,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: SetUserBannedAt :exec
UPDATE lic.users SET banned_at = @banned_at, updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteUserTokens :exec
DELETE FROM lic.tokens
WHERE account_id = @account_id AND bearer_type = 'User' AND bearer_id = @user_id;

-- name: DeleteUserTokensExcept :exec
DELETE FROM lic.tokens
WHERE account_id = @account_id AND bearer_type = 'User' AND bearer_id = @user_id
  AND id <> @except_id;

-- name: SetUserPasswordReset :exec
UPDATE lic.users SET
  password_reset_token = @password_reset_token,
  password_reset_sent_at = @password_reset_sent_at
WHERE account_id = @account_id AND id = @id;

-- name: CountGroupUsers :one
SELECT count(*)::int AS total FROM lic.users
WHERE account_id = @account_id AND group_id = @group_id;

-- name: UserIsTeammate :one
SELECT EXISTS (
  SELECT 1
  FROM lic.licenses l
  WHERE l.account_id = @account_id
    AND (
      (l.user_id = @user_id AND EXISTS (
        SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @other_id
      ))
      OR (l.user_id = @other_id AND EXISTS (
        SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @user_id
      ))
    )
) AS teammate;

-- name: UserOfLicense :one
SELECT EXISTS (
  SELECT 1
  FROM lic.licenses l
  WHERE l.account_id = @account_id AND l.id = @license_id
    AND (l.user_id = @user_id OR EXISTS (
      SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @user_id
    ))
) AS member;

-- name: UserLicenseIDs :many
SELECT l.id
FROM lic.licenses l
WHERE l.account_id = @account_id
  AND (
    l.user_id = @user_id
    OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @user_id)
  )
ORDER BY l.created_at DESC, l.id DESC;

-- name: UserMachineIDs :many
SELECT m.id
FROM lic.machines m
WHERE m.account_id = @account_id
  AND (
    m.owner_id = @user_id
    OR EXISTS (SELECT 1 FROM lic.licenses l WHERE l.id = m.license_id AND l.user_id = @user_id)
    OR EXISTS (
      SELECT 1
      FROM lic.license_users lu
      JOIN lic.licenses l ON l.id = lu.license_id
      WHERE l.id = m.license_id AND lu.user_id = @user_id
    )
  )
ORDER BY m.created_at DESC, m.id DESC;

-- name: UserProductIDs :many
SELECT DISTINCT l.product_id AS id
FROM lic.licenses l
WHERE l.account_id = @account_id
  AND (
    l.user_id = @user_id
    OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = @user_id)
  );

-- name: GroupIDsOfUser :many
SELECT DISTINCT g.id
FROM lic.groups g
WHERE g.account_id = @account_id
  AND (
    g.id = (SELECT u.group_id FROM lic.users u WHERE u.id = @user_id AND u.group_id IS NOT NULL)
    OR EXISTS (SELECT 1 FROM lic.group_owners go WHERE go.group_id = g.id AND go.user_id = @user_id)
  );
