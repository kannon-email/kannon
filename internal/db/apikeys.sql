-- name: CreateAPIKey :one
INSERT INTO api_keys (id, domain, key_hash, key_prefix, name, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
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
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAPIKeysByDomain :one
SELECT COUNT(*)
FROM api_keys
WHERE domain = $1
    AND (CASE WHEN $2::boolean THEN is_active = TRUE ELSE TRUE END);
