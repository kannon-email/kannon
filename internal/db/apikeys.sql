-- name: CreateAPIKey :one
INSERT INTO api_keys (id, domain, key, name, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateAPIKey :one
UPDATE api_keys
SET name = $3,
    expires_at = $4,
    is_active = $5,
    deactivated_at = $6
WHERE id = $1 AND domain = $2
RETURNING *;

-- name: GetAPIKeyByKey :one
SELECT *
FROM api_keys
WHERE key = $1 AND domain = $2;

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

-- name: ValidateAPIKeyForAuth :one
SELECT *
FROM api_keys
WHERE domain = $1
    AND key = $2
    AND is_active = TRUE
    AND (expires_at IS NULL OR expires_at > NOW());
