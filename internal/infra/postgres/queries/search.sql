-- POST /search: one query per searchable attribute, each answering the
-- matching ids of the account. The app layer combines them (AND/OR) and
-- hands the set to the resource's ordinary index.
--
-- Shared forms: an id matches exactly when the term is a UUID, else as a
-- substring; a relationship accepts its id, a display value (ILIKE) or,
-- for terms made of UUID characters, an id prefix (tsquery prefix match).

-- name: SearchLicensesByID :many
SELECT id FROM lic.licenses
WHERE account_id = @account_id
  AND (id = sqlc.narg(id)::uuid OR (sqlc.narg(id)::uuid IS NULL AND id::text ILIKE '%' || @like::text || '%'));

-- name: SearchLicensesByKey :many
SELECT id FROM lic.licenses WHERE account_id = @account_id AND key = @term::text;

-- name: SearchLicensesByName :many
SELECT id FROM lic.licenses WHERE account_id = @account_id AND name ILIKE '%' || @like::text || '%';

-- name: SearchLicensesByMetadata :many
SELECT id FROM lic.licenses
WHERE account_id = @account_id AND (metadata @> @raw::jsonb OR metadata @> @cast::jsonb);

-- name: SearchLicensesByOwner :many
SELECT l.id FROM lic.licenses l
LEFT JOIN lic.users u ON u.id = l.user_id
WHERE l.account_id = @account_id
  AND (
    l.user_id = sqlc.narg(id)::uuid
    OR (sqlc.narg(id)::uuid IS NULL AND u.id IS NOT NULL AND (
      u.email = @term::text
      OR u.email ILIKE '%' || @like::text || '%'
      OR (@prefix::bool AND to_tsvector('simple', u.id::text) @@ to_tsquery('simple', ''' ' || @tsv::text || ' ''' || ':*'))
    ))
  );

-- name: SearchLicensesByUser :many
-- A license's users are its owner and its licensees.
SELECT DISTINCT l.id FROM lic.licenses l
JOIN lic.users u ON u.account_id = l.account_id AND (
  u.id = l.user_id OR EXISTS (SELECT 1 FROM lic.license_users lu WHERE lu.license_id = l.id AND lu.user_id = u.id)
)
WHERE l.account_id = @account_id
  AND (
    u.id = sqlc.narg(id)::uuid
    OR (sqlc.narg(id)::uuid IS NULL AND (
      u.email = @term::text
      OR u.email ILIKE '%' || @like::text || '%'
      OR (@prefix::bool AND to_tsvector('simple', u.id::text) @@ to_tsquery('simple', ''' ' || @tsv::text || ' ''' || ':*'))
    ))
  );

-- name: SearchLicensesByProduct :many
SELECT l.id FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
JOIN lic.products pr ON pr.id = p.product_id
WHERE l.account_id = @account_id
  AND (
    p.product_id = sqlc.narg(id)::uuid
    OR (sqlc.narg(id)::uuid IS NULL AND (
      pr.name ILIKE '%' || @like::text || '%'
      OR (@prefix::bool AND to_tsvector('simple', pr.id::text) @@ to_tsquery('simple', ''' ' || @tsv::text || ' ''' || ':*'))
    ))
  );

-- name: SearchLicensesByPolicy :many
SELECT l.id FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
WHERE l.account_id = @account_id
  AND (
    l.policy_id = sqlc.narg(id)::uuid
    OR (sqlc.narg(id)::uuid IS NULL AND (
      p.name ILIKE '%' || @like::text || '%'
      OR (@prefix::bool AND to_tsvector('simple', l.policy_id::text) @@ to_tsquery('simple', ''' ' || @tsv::text || ' ''' || ':*'))
    ))
  );

-- name: SearchUsersByID :many
SELECT id FROM lic.users
WHERE account_id = @account_id
  AND (id = sqlc.narg(id)::uuid OR (sqlc.narg(id)::uuid IS NULL AND id::text ILIKE '%' || @like::text || '%'));

-- name: SearchUsersByEmail :many
SELECT id FROM lic.users WHERE account_id = @account_id AND email ILIKE '%' || @like::text || '%';

-- name: SearchUsersByFirstName :many
SELECT id FROM lic.users WHERE account_id = @account_id AND first_name ILIKE '%' || @like::text || '%';

-- name: SearchUsersByLastName :many
SELECT id FROM lic.users WHERE account_id = @account_id AND last_name ILIKE '%' || @like::text || '%';

-- name: SearchUsersByMetadata :many
SELECT id FROM lic.users
WHERE account_id = @account_id AND (metadata @> @raw::jsonb OR metadata @> @cast::jsonb);

-- name: SearchUsersByRole :many
SELECT id FROM lic.users WHERE account_id = @account_id AND role ILIKE '%' || @like::text || '%';

-- name: SearchProductsByID :many
SELECT id FROM lic.products
WHERE account_id = @account_id
  AND (id = sqlc.narg(id)::uuid OR (sqlc.narg(id)::uuid IS NULL AND id::text ILIKE '%' || @like::text || '%'));

-- name: SearchProductsByName :many
SELECT id FROM lic.products WHERE account_id = @account_id AND name ILIKE '%' || @like::text || '%';

-- name: SearchProductsByMetadata :many
SELECT id FROM lic.products
WHERE account_id = @account_id AND (metadata @> @raw::jsonb OR metadata @> @cast::jsonb);

-- name: SearchPoliciesByID :many
SELECT id FROM lic.policies
WHERE account_id = @account_id
  AND (id = sqlc.narg(id)::uuid OR (sqlc.narg(id)::uuid IS NULL AND id::text ILIKE '%' || @like::text || '%'));

-- name: SearchPoliciesByName :many
SELECT id FROM lic.policies WHERE account_id = @account_id AND name ILIKE '%' || @like::text || '%';

-- name: SearchPoliciesByMetadata :many
SELECT id FROM lic.policies
WHERE account_id = @account_id AND (metadata @> @raw::jsonb OR metadata @> @cast::jsonb);

-- name: SearchPoliciesByProduct :many
SELECT p.id FROM lic.policies p
JOIN lic.products pr ON pr.id = p.product_id
WHERE p.account_id = @account_id
  AND (
    p.product_id = sqlc.narg(id)::uuid
    OR (sqlc.narg(id)::uuid IS NULL AND (
      pr.name ILIKE '%' || @like::text || '%'
      OR (@prefix::bool AND to_tsvector('simple', p.product_id::text) @@ to_tsquery('simple', ''' ' || @tsv::text || ' ''' || ':*'))
    ))
  );

-- name: SearchGroupsByName :many
SELECT id FROM lic.groups WHERE account_id = @account_id AND name ILIKE '%' || @like::text || '%';

-- name: SearchEntitlementsByID :many
SELECT id FROM lic.entitlements
WHERE account_id = @account_id
  AND (id = sqlc.narg(id)::uuid OR (sqlc.narg(id)::uuid IS NULL AND id::text ILIKE '%' || @like::text || '%'));

-- name: SearchEntitlementsByCode :many
SELECT id FROM lic.entitlements WHERE account_id = @account_id AND code ILIKE '%' || @like::text || '%';

-- name: SearchEntitlementsByName :many
SELECT id FROM lic.entitlements WHERE account_id = @account_id AND name ILIKE '%' || @like::text || '%';
