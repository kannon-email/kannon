-- name: GetDomains :many
SELECT * FROM domains;

-- name: FindDomain :one
SELECT
    *
FROM domains
    WHERE domain = $1
;

-- name: GetAllDomains :many
SELECT
    *
FROM domains
;

-- name: CreateDomain :one
INSERT INTO domains
    (domain, dkim_private_key, dkim_public_key)
    VALUES ($1, $2, $3)
    RETURNING *;

-- name: FindTemplate :one
SELECT * FROM templates
WHERE template_id = $1
AND domain = $2;