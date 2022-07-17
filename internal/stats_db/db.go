// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.13.0

package sq

import (
	"context"
	"database/sql"
	"fmt"
)

type DBTX interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func New(db DBTX) *Queries {
	return &Queries{db: db}
}

func Prepare(ctx context.Context, db DBTX) (*Queries, error) {
	q := Queries{db: db}
	var err error
	if q.insertAcceptedStmt, err = db.PrepareContext(ctx, insertAccepted); err != nil {
		return nil, fmt.Errorf("error preparing query InsertAccepted: %w", err)
	}
	if q.insertClickStmt, err = db.PrepareContext(ctx, insertClick); err != nil {
		return nil, fmt.Errorf("error preparing query InsertClick: %w", err)
	}
	if q.insertHardBouncedStmt, err = db.PrepareContext(ctx, insertHardBounced); err != nil {
		return nil, fmt.Errorf("error preparing query InsertHardBounced: %w", err)
	}
	if q.insertOpenStmt, err = db.PrepareContext(ctx, insertOpen); err != nil {
		return nil, fmt.Errorf("error preparing query InsertOpen: %w", err)
	}
	if q.insertPreparedStmt, err = db.PrepareContext(ctx, insertPrepared); err != nil {
		return nil, fmt.Errorf("error preparing query InsertPrepared: %w", err)
	}
	return &q, nil
}

func (q *Queries) Close() error {
	var err error
	if q.insertAcceptedStmt != nil {
		if cerr := q.insertAcceptedStmt.Close(); cerr != nil {
			err = fmt.Errorf("error closing insertAcceptedStmt: %w", cerr)
		}
	}
	if q.insertClickStmt != nil {
		if cerr := q.insertClickStmt.Close(); cerr != nil {
			err = fmt.Errorf("error closing insertClickStmt: %w", cerr)
		}
	}
	if q.insertHardBouncedStmt != nil {
		if cerr := q.insertHardBouncedStmt.Close(); cerr != nil {
			err = fmt.Errorf("error closing insertHardBouncedStmt: %w", cerr)
		}
	}
	if q.insertOpenStmt != nil {
		if cerr := q.insertOpenStmt.Close(); cerr != nil {
			err = fmt.Errorf("error closing insertOpenStmt: %w", cerr)
		}
	}
	if q.insertPreparedStmt != nil {
		if cerr := q.insertPreparedStmt.Close(); cerr != nil {
			err = fmt.Errorf("error closing insertPreparedStmt: %w", cerr)
		}
	}
	return err
}

func (q *Queries) exec(ctx context.Context, stmt *sql.Stmt, query string, args ...interface{}) (sql.Result, error) {
	switch {
	case stmt != nil && q.tx != nil:
		return q.tx.StmtContext(ctx, stmt).ExecContext(ctx, args...)
	case stmt != nil:
		return stmt.ExecContext(ctx, args...)
	default:
		return q.db.ExecContext(ctx, query, args...)
	}
}

func (q *Queries) query(ctx context.Context, stmt *sql.Stmt, query string, args ...interface{}) (*sql.Rows, error) {
	switch {
	case stmt != nil && q.tx != nil:
		return q.tx.StmtContext(ctx, stmt).QueryContext(ctx, args...)
	case stmt != nil:
		return stmt.QueryContext(ctx, args...)
	default:
		return q.db.QueryContext(ctx, query, args...)
	}
}

func (q *Queries) queryRow(ctx context.Context, stmt *sql.Stmt, query string, args ...interface{}) *sql.Row {
	switch {
	case stmt != nil && q.tx != nil:
		return q.tx.StmtContext(ctx, stmt).QueryRowContext(ctx, args...)
	case stmt != nil:
		return stmt.QueryRowContext(ctx, args...)
	default:
		return q.db.QueryRowContext(ctx, query, args...)
	}
}

type Queries struct {
	db                    DBTX
	tx                    *sql.Tx
	insertAcceptedStmt    *sql.Stmt
	insertClickStmt       *sql.Stmt
	insertHardBouncedStmt *sql.Stmt
	insertOpenStmt        *sql.Stmt
	insertPreparedStmt    *sql.Stmt
}

func (q *Queries) WithTx(tx *sql.Tx) *Queries {
	return &Queries{
		db:                    tx,
		tx:                    tx,
		insertAcceptedStmt:    q.insertAcceptedStmt,
		insertClickStmt:       q.insertClickStmt,
		insertHardBouncedStmt: q.insertHardBouncedStmt,
		insertOpenStmt:        q.insertOpenStmt,
		insertPreparedStmt:    q.insertPreparedStmt,
	}
}
