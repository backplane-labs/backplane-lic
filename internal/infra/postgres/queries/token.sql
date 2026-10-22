-- Tokens (internal/app/authn, internal/app/tokens).

-- name: GetTokenByDigest :one
SELECT id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions, created_at, updated_at
FROM lic.tokens
WHERE account_id = @account_id AND digest = @digest;

-- name: InsertToken :exec
INSERT INTO lic.tokens (
  id, account_id, digest, bearer_id, bearer_type, bearer_role, kind,
  name, expiry, permissions
) VALUES (
  @id, @account_id, @digest, @bearer_id, @bearer_type, @bearer_role, @kind,
  @name, @expiry, @permissions
);
