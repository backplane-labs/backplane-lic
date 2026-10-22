-- Groups (internal/app/groups).

-- name: GetGroupByID :one
SELECT id, account_id, name, max_users, max_licenses, max_machines, metadata,
  created_at, updated_at
FROM lic.groups
WHERE account_id = @account_id AND id = @id;

-- name: InsertGroup :exec
INSERT INTO lic.groups (
  id, account_id, name, max_users, max_licenses, max_machines, metadata
) VALUES (
  @id, @account_id, @name, @max_users, @max_licenses, @max_machines, @metadata
);

-- name: UpdateGroupColumns :exec
UPDATE lic.groups SET
  name = @name,
  max_users = @max_users,
  max_licenses = @max_licenses,
  max_machines = @max_machines,
  metadata = @metadata,
  updated_at = now()
WHERE account_id = @account_id AND id = @id;

-- name: DeleteGroup :exec
DELETE FROM lic.groups WHERE account_id = @account_id AND id = @id;

-- name: CountGroups :one
SELECT count(*)::int AS total FROM lic.groups
WHERE account_id = @account_id
  AND (
    @member_user::uuid IS NULL
    OR EXISTS (SELECT 1 FROM lic.users u WHERE u.group_id = lic.groups.id AND u.id = @member_user)
  );

-- name: ListGroups :many
SELECT id, account_id, name, max_users, max_licenses, max_machines, metadata,
  created_at, updated_at
FROM lic.groups
WHERE account_id = @account_id
  AND (
    @member_user::uuid IS NULL
    OR EXISTS (SELECT 1 FROM lic.users u WHERE u.group_id = lic.groups.id AND u.id = @member_user)
  )
  AND (
    @member_license::uuid IS NULL
    OR EXISTS (
      SELECT 1 FROM lic.licenses l WHERE l.group_id = lic.groups.id AND l.id = @member_license
    )
  )
  AND (
    @cursor::uuid IS NULL
    OR CASE
      WHEN @asc::boolean THEN
        (created_at, id) > (SELECT c.created_at, c.id FROM lic.groups c WHERE c.id = @cursor)
      ELSE
        (created_at, id) < (SELECT c.created_at, c.id FROM lic.groups c WHERE c.id = @cursor)
    END
  )
ORDER BY
  CASE WHEN @asc::boolean THEN created_at END ASC,
  CASE WHEN @asc::boolean THEN id END ASC,
  created_at DESC,
  id DESC
LIMIT @limit
OFFSET @offset;

-- name: GetGroupByUser :one
SELECT g.id, g.account_id, g.name, g.max_users, g.max_licenses, g.max_machines,
  g.metadata, g.created_at, g.updated_at
FROM lic.groups g
JOIN lic.users u ON u.group_id = g.id
WHERE g.account_id = @account_id AND u.id = @user_id;

-- name: InsertGroupOwner :exec
INSERT INTO lic.group_owners (id, account_id, group_id, user_id)
VALUES (@id, @account_id, @group_id, @user_id);

-- name: GetGroupOwner :one
SELECT id, account_id, group_id, user_id, created_at, updated_at
FROM lic.group_owners
WHERE account_id = @account_id AND group_id = @group_id AND id = @id;

-- name: GetGroupOwnerByUser :one
SELECT id, account_id, group_id, user_id, created_at, updated_at
FROM lic.group_owners
WHERE account_id = @account_id AND group_id = @group_id AND user_id = @user_id;

-- name: ListGroupOwners :many
SELECT id, account_id, group_id, user_id, created_at, updated_at
FROM lic.group_owners
WHERE account_id = @account_id AND group_id = @group_id
ORDER BY created_at DESC, id DESC
LIMIT @limit;

-- name: DeleteGroupOwnersByUsers :execrows
DELETE FROM lic.group_owners
WHERE account_id = @account_id AND group_id = @group_id AND user_id = ANY(@user_ids::uuid[]);

-- name: ListGroupUsers :many
SELECT u.id, u.account_id, u.first_name, u.last_name, u.email, u.password_digest,
  u.role, u.permissions, u.group_id, u.metadata, u.banned_at, u.created_at, u.updated_at
FROM lic.users u
WHERE u.account_id = @account_id AND u.group_id = @group_id
ORDER BY u.created_at DESC, u.id DESC
LIMIT @limit;

-- name: ListGroupLicenses :many
SELECT l.id
FROM lic.licenses l
WHERE l.account_id = @account_id AND l.group_id = @group_id
ORDER BY l.created_at DESC, l.id DESC
LIMIT @limit;

-- name: ListGroupMachines :many
SELECT m.id
FROM lic.machines m
WHERE m.account_id = @account_id AND m.group_id = @group_id
ORDER BY m.created_at DESC, m.id DESC
LIMIT @limit;
