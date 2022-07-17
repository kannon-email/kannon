// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.13.0
// source: stats.sql

package sq

import (
	"context"
	"time"
)

const insertAccepted = `-- name: InsertAccepted :exec
INSERT INTO accepted (email, message_id, timestamp, domain) VALUES ($1, $2, $3, $4)
`

type InsertAcceptedParams struct {
	Email     string
	MessageID string
	Timestamp time.Time
	Domain    string
}

func (q *Queries) InsertAccepted(ctx context.Context, arg InsertAcceptedParams) error {
	_, err := q.exec(ctx, q.insertAcceptedStmt, insertAccepted,
		arg.Email,
		arg.MessageID,
		arg.Timestamp,
		arg.Domain,
	)
	return err
}

const insertHardBounced = `-- name: InsertHardBounced :exec
INSERT INTO hard_bounced (email, message_id, timestamp, domain, err_code, err_msg) VALUES  ($1, $2, $3, $4, $5, $6)
`

type InsertHardBouncedParams struct {
	Email     string
	MessageID string
	Timestamp time.Time
	Domain    string
	ErrCode   int32
	ErrMsg    string
}

func (q *Queries) InsertHardBounced(ctx context.Context, arg InsertHardBouncedParams) error {
	_, err := q.exec(ctx, q.insertHardBouncedStmt, insertHardBounced,
		arg.Email,
		arg.MessageID,
		arg.Timestamp,
		arg.Domain,
		arg.ErrCode,
		arg.ErrMsg,
	)
	return err
}

const insertOpen = `-- name: InsertOpen :exec
INSERT INTO open (email, message_id, timestamp, domain, ip, user_agent) VALUES  ($1, $2, $3, $4, $5, $6)
`

type InsertOpenParams struct {
	Email     string
	MessageID string
	Timestamp time.Time
	Domain    string
	Ip        string
	UserAgent string
}

func (q *Queries) InsertOpen(ctx context.Context, arg InsertOpenParams) error {
	_, err := q.exec(ctx, q.insertOpenStmt, insertOpen,
		arg.Email,
		arg.MessageID,
		arg.Timestamp,
		arg.Domain,
		arg.Ip,
		arg.UserAgent,
	)
	return err
}

const insertPrepared = `-- name: InsertPrepared :exec
INSERT INTO prepared (email, message_id, timestamp, first_timestamp, domain) VALUES ($1, $2, $3, $3, $4)
	ON CONFLICT (email, message_id, domain) DO UPDATE
	SET timestamp = $3
`

type InsertPreparedParams struct {
	Email     string
	MessageID string
	Timestamp time.Time
	Domain    string
}

func (q *Queries) InsertPrepared(ctx context.Context, arg InsertPreparedParams) error {
	_, err := q.exec(ctx, q.insertPreparedStmt, insertPrepared,
		arg.Email,
		arg.MessageID,
		arg.Timestamp,
		arg.Domain,
	)
	return err
}
