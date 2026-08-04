package sqlc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/audit"
)

// AuditRepository implements audit.Repository using sqlc queries. Write-only, because the interface
// is: nothing in Kannon reads an Audit Record back, so there is no query here to be tempted into
// letting an authorization decision consult the register describing earlier ones (ADR 0010).
type AuditRepository struct {
	db *pgxpool.Pool
}

func NewAuditRepository(db *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Insert(ctx context.Context, rec audit.Record) error {
	// The Details go to the jsonb column as the same JSON they crossed NATS as, which is the whole
	// point of the column's type: what a decision records can grow without a migration, and the
	// shape is not restated anywhere between the producer and the row.
	data, err := json.Marshal(rec.Details)
	if err != nil {
		return fmt.Errorf("cannot marshal details of audit record %s: %w", rec.ID, err)
	}

	q := New(r.db)
	return q.InsertAuditRecord(ctx, InsertAuditRecordParams{
		ID:         rec.ID,
		OccurredAt: toPgTimestamptz(rec.OccurredAt),
		Principal:  rec.Principal,
		Resource:   rec.Resource,
		Action:     rec.Action,
		// The Outcome is cast rather than overridden in sqlc.yaml. It arrives already validated —
		// a payload naming an outcome outside the vocabulary never becomes a Record — and a
		// generated type here would only add a second place the vocabulary is written down.
		Outcome: string(rec.Outcome),
		Data:    data,
	})
}

func (r *AuditRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	q := New(r.db)
	return q.DeleteAuditRecordsOlderThan(ctx, toPgTimestamptz(before))
}

// toPgTimestamptz is a sibling of toPgTimestamp rather than a replacement for it: occurred_at is
// the only timestamptz column in the schema, and the timezone-naive ones stay as they are. The
// distinction is real. Every other instant in Kannon is written by the process that read it out of
// its own database, so a naive column loses nothing; an Audit Record's instant is stamped by the
// producer and crosses a process boundary as JSON before anything stores it, so the offset has to
// travel with it or a consumer running elsewhere records the wrong moment.
func toPgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  t,
		Valid: true,
	}
}
