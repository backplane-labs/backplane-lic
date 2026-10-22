-- Account bootstrap (internal/app/accounts).

-- name: GetAccount :one
SELECT id, name, slug, api_version, protected, secret_key, private_key, public_key,
  ed25519_private_key, ed25519_public_key, ecdsa_private_key, ecdsa_public_key,
  created_at, updated_at
FROM lic.accounts
WHERE id = @id;

-- name: InsertAccount :exec
INSERT INTO lic.accounts (
  id, name, slug, api_version, protected,
  secret_key, private_key, public_key,
  ed25519_private_key, ed25519_public_key,
  ecdsa_private_key, ecdsa_public_key
) VALUES (
  @id, @name, @slug, @api_version, @protected,
  @secret_key, @private_key, @public_key,
  @ed25519_private_key, @ed25519_public_key,
  @ecdsa_private_key, @ecdsa_public_key
);
