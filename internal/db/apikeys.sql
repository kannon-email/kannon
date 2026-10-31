-- name: CreateAPIKey :one
INSERT INTO api_keys (id, domain, key_hash, key_prefix, name, expires_at, roles)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateAPIKey :one
UPDATE api_keys
SET name = $3,
    expires_at = $4,
    is_active = $5,
    deactivated_at = $6
WHERE id = $1 AND domain = $2
RETURNING *;

-- name: GetAPIKeyByHash :one
SELECT *
FROM api_keys
WHERE key_hash = $1 AND domain = $2;

-- name: GetAPIKeyByID :one
SELECT *
FROM api_keys
WHERE id = $1 AND domain = $2;

-- name: GetAPIKeyByIDForUpdate :one
SELECT *
FROM api_keys
WHERE id = $1 AND domain = $2
FOR UPDATE;

-- name: ListAPIKeysByDomain :many
SELECT *
FROM api_keys
WHERE domain = $1
    AND (CASE WHEN $2::boolean THEN is_active = TRUE ELSE TRUE END)
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4;

-- name: ListAPIKeysPage :many
SELECT *
FROM api_keys
WHERE domain = @domain
    AND (sqlc.narg(before_time)::timestamp IS NULL OR (created_at, id) < (sqlc.narg(before_time)::timestamp, sqlc.arg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @take;

-- name: CountAPIKeysByDomain :one
SELECT COUNT(*)
FROM api_keys
WHERE domain = $1
    AND (CASE WHEN $2::boolean THEN is_active = TRUE ELSE TRUE END);

-- name: FindAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = $1;
