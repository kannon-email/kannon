package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/batch"
)

type batchRepository struct {
	db *pgxpool.Pool
}

// NewBatchRepository creates a new PostgreSQL-backed Batch repository.
// It writes to and reads from the messages table.
func NewBatchRepository(db *pgxpool.Pool) batch.Repository {
	return &batchRepository{db: db}
}

func (r *batchRepository) Create(ctx context.Context, b *batch.Batch) error {
	q := New(r.db)
	_, err := q.CreateMessage(ctx, CreateMessageParams{
		MessageID:   b.ID().String(),
		Subject:     b.Subject(),
		SenderEmail: b.Sender().Email,
		SenderAlias: b.Sender().Alias,
		TemplateID:  b.TemplateID(),
		Domain:      b.Domain(),
		Attachments: toSQLCAttachments(b.Attachments()),
		Headers:     toSQLCHeaders(b.Headers()),
		Tracking:    b.TrackingPolicy(),
	})
	return err
}

func (r *batchRepository) GetByID(ctx context.Context, id batch.ID) (*batch.Batch, error) {
	q := New(r.db)
	row, err := q.GetMessage(ctx, id.String())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, batch.ErrBatchNotFound
		}
		return nil, err
	}
	return rowToBatch(row), nil
}

func rowToBatch(row Message) *batch.Batch {
	return batch.Load(batch.LoadParams{
		ID:      batch.ID(row.MessageID),
		Subject: row.Subject,
		Sender: batch.Sender{
			Email: row.SenderEmail,
			Alias: row.SenderAlias,
		},
		TemplateID:  row.TemplateID,
		Domain:      row.Domain,
		Attachments: fromSQLCAttachments(row.Attachments),
		Headers:     fromSQLCHeaders(row.Headers),
		Tracking:    row.Tracking,
	})
}

func toSQLCAttachments(a batch.Attachments) Attachments {
	if a == nil {
		return Attachments{}
	}
	out := make(Attachments, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

func fromSQLCAttachments(a Attachments) batch.Attachments {
	if a == nil {
		return nil
	}
	out := make(batch.Attachments, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

func toSQLCHeaders(h batch.Headers) Headers {
	return Headers{To: h.To, Cc: h.Cc}
}

func fromSQLCHeaders(h Headers) batch.Headers {
	return batch.Headers{To: h.To, Cc: h.Cc}
}
