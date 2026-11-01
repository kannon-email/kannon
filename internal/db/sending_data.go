package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/envelope"
)

type sendingDataSource struct {
	db DBTX
}

// NewSendingDataSource creates the PostgreSQL-backed source of a Batch's rendering inputs. It
// resolves the Batch + Template + Domain join in a single query (GetSendingData in pool.sql).
func NewSendingDataSource(db *pgxpool.Pool) envelope.SendingDataSource {
	return &sendingDataSource{db: db}
}

func (s *sendingDataSource) GetSendingData(ctx context.Context, batchID batch.ID) (envelope.SendingData, error) {
	row, err := New(s.db).GetSendingData(ctx, batchID.String())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return envelope.SendingData{}, batch.ErrBatchNotFound
		}
		return envelope.SendingData{}, err
	}

	atts := make(map[string][]byte, len(row.Attachments))
	for name, raw := range row.Attachments {
		atts[name] = raw
	}

	return envelope.SendingData{
		LiteralFields:  row.LiteralFields,
		Subject:        row.Subject,
		HTML:           row.Html,
		Domain:         row.Domain,
		MessageID:      row.MessageID,
		SenderEmail:    row.SenderEmail,
		SenderAlias:    row.SenderAlias,
		DkimPrivateKey: row.DkimPrivateKey,
		Attachments:    atts,
		Headers: batch.Headers{
			To: row.Headers.To,
			Cc: row.Headers.Cc,
		},
		// A Batch written before ADR 0005 has no unsubscribe key in its headers, which is
		// indistinguishable from, and treated as, a Batch that states no endpoint.
		OneClickUnsubscribe: fromSQLCUnsubscribe(row.Headers),
		// Likewise a Batch written before Reply-To could be stated, which states none.
		ReplyTo: fromSQLCReplyTo(row.Headers),
	}, nil
}
