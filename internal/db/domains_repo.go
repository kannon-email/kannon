package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/kannon-email/kannon/internal/domains"
)

type domainsRepository struct {
	q *Queries
}

// NewDomainsRepository creates a new PostgreSQL-backed SenderDomain repository.
func NewDomainsRepository(q *Queries) domains.Repository {
	return &domainsRepository{q: q}
}

func (r *domainsRepository) Create(ctx context.Context, d *domains.Domain) error {
	row, err := r.q.CreateDomain(ctx, CreateDomainParams{
		Domain:         d.Domain(),
		DkimPrivateKey: d.DkimPrivateKey(),
		DkimPublicKey:  d.DkimPublicKey(),
	})
	if err != nil {
		return err
	}
	*d = *rowToDomain(row)
	return nil
}

func (r *domainsRepository) FindByName(ctx context.Context, fqdn string) (*domains.Domain, error) {
	row, err := r.q.FindDomain(ctx, fqdn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domains.ErrDomainNotFound
		}
		return nil, err
	}
	return rowToDomain(row), nil
}

func (r *domainsRepository) List(ctx context.Context) ([]*domains.Domain, error) {
	rows, err := r.q.GetAllDomains(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*domains.Domain, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToDomain(row))
	}
	return out, nil
}

func rowToDomain(row Domain) *domains.Domain {
	return domains.Load(domains.LoadParams{
		ID:             row.ID,
		Domain:         row.Domain,
		DkimPrivateKey: row.DkimPrivateKey,
		DkimPublicKey:  row.DkimPublicKey,
		CreatedAt:      row.CreatedAt.Time,
	})
}
