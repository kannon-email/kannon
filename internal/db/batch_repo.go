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
		Headers:     toSQLCHeaders(b.Headers(), b.OneClickUnsubscribe()),
		Tracking:    b.TrackingPolicy(),
	})
	// The only key a new Batch can break is the one it holds on its Template, so
	// a refusal here is a Template that went away between intake looking it up
	// and this insert (ADR 0008).
	if isForeignKeyViolation(err) {
		return batch.ErrTemplateMissing
	}
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
		TemplateID:          row.TemplateID,
		Domain:              row.Domain,
		Attachments:         fromSQLCAttachments(row.Attachments),
		Headers:             fromSQLCHeaders(row.Headers),
		OneClickUnsubscribe: fromSQLCUnsubscribe(row.Headers),
		Tracking:            row.Tracking,
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

// toSQLCHeaders folds both header-shaped statements of a Batch into the single
// JSONB column that holds them. They are separate concepts in the domain but
// share one column, so the mapping is the one place that knows it.
func toSQLCHeaders(h batch.Headers, u batch.OneClickUnsubscribe) Headers {
	out := Headers{To: h.To, Cc: h.Cc}
	if !u.IsZero() {
		out.OneClickUnsubscribe = &OneClickUnsubscribe{URLTemplate: u.URLTemplate}
	}
	return out
}

func fromSQLCHeaders(h Headers) batch.Headers {
	return batch.Headers{To: h.To, Cc: h.Cc}
}

// fromSQLCUnsubscribe returns the zero value for a Batch stored before the key
// existed, which is the same thing as a Batch that states no endpoint.
func fromSQLCUnsubscribe(h Headers) batch.OneClickUnsubscribe {
	if h.OneClickUnsubscribe == nil {
		return batch.OneClickUnsubscribe{}
	}
	return batch.OneClickUnsubscribe{URLTemplate: h.OneClickUnsubscribe.URLTemplate}
}
