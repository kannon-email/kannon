-- name: GetValidStatsKeys :one
SELECT * FROM stats_keys WHERE expiration_time > $1 LIMIT 1;

-- name: CreateStatsKeys :one
INSERT INTO stats_keys 
	(id, private_key, public_key, creation_time, expiration_time) 
	VALUES ($1, $2, $3, NOW(), $4) RETURNING *;

-- name: GetValidPublicStatsKeyByKid :one
SELECT id, public_key, expiration_time FROM stats_keys WHERE expiration_time > NOW() AND id=$1;



