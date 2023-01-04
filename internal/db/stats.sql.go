// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.16.0
// source: stats.sql

package sqlc

import (
	"context"
	"time"
)

const createStatsKeys = `-- name: CreateStatsKeys :one
INSERT INTO stats_keys 
	(id, private_key, public_key, creation_time, expiration_time) 
	VALUES ($1, $2, $3, NOW(), $4) RETURNING id, private_key, public_key, creation_time, expiration_time
`

type CreateStatsKeysParams struct {
	ID             string
	PrivateKey     string
	PublicKey      string
	ExpirationTime time.Time
}

func (q *Queries) CreateStatsKeys(ctx context.Context, arg CreateStatsKeysParams) (StatsKey, error) {
	row := q.queryRow(ctx, q.createStatsKeysStmt, createStatsKeys,
		arg.ID,
		arg.PrivateKey,
		arg.PublicKey,
		arg.ExpirationTime,
	)
	var i StatsKey
	err := row.Scan(
		&i.ID,
		&i.PrivateKey,
		&i.PublicKey,
		&i.CreationTime,
		&i.ExpirationTime,
	)
	return i, err
}

const getValidPublicStatsKeyByKid = `-- name: GetValidPublicStatsKeyByKid :one
SELECT id, public_key, expiration_time FROM stats_keys WHERE expiration_time > NOW() AND id=$1
`

type GetValidPublicStatsKeyByKidRow struct {
	ID             string
	PublicKey      string
	ExpirationTime time.Time
}

func (q *Queries) GetValidPublicStatsKeyByKid(ctx context.Context, id string) (GetValidPublicStatsKeyByKidRow, error) {
	row := q.queryRow(ctx, q.getValidPublicStatsKeyByKidStmt, getValidPublicStatsKeyByKid, id)
	var i GetValidPublicStatsKeyByKidRow
	err := row.Scan(&i.ID, &i.PublicKey, &i.ExpirationTime)
	return i, err
}

const getValidStatsKeys = `-- name: GetValidStatsKeys :one
SELECT id, private_key, public_key, creation_time, expiration_time FROM stats_keys WHERE expiration_time > $1 LIMIT 1
`

func (q *Queries) GetValidStatsKeys(ctx context.Context, expirationTime time.Time) (StatsKey, error) {
	row := q.queryRow(ctx, q.getValidStatsKeysStmt, getValidStatsKeys, expirationTime)
	var i StatsKey
	err := row.Scan(
		&i.ID,
		&i.PrivateKey,
		&i.PublicKey,
		&i.CreationTime,
		&i.ExpirationTime,
	)
	return i, err
}
