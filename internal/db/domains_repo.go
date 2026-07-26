package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tracking"
)

type domainsRepository struct {
	db *pgxpool.Pool
}

// NewDomainsRepository creates a new PostgreSQL-backed SenderDomain repository.
func NewDomainsRepository(db *pgxpool.Pool) domains.Repository {
	return &domainsRepository{db: db}
}

func (r *domainsRepository) Create(ctx context.Context, d *domains.Domain) error {
	q := New(r.db)
	row, err := q.CreateDomain(ctx, CreateDomainParams{
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

func (r *domainsRepository) SetTrackingPolicy(ctx context.Context, fqdn string, p tracking.Policy) (*domains.Domain, error) {
	q := New(r.db)
	row, err := q.SetDomainTracking(ctx, SetDomainTrackingParams{
		Domain:   fqdn,
		Tracking: p.Normalized(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domains.ErrDomainNotFound
		}
		return nil, err
	}
	return rowToDomain(row), nil
}

func (r *domainsRepository) FindByName(ctx context.Context, fqdn string) (*domains.Domain, error) {
	q := New(r.db)
	row, err := q.FindDomain(ctx, fqdn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domains.ErrDomainNotFound
		}
		return nil, err
	}
	return rowToDomain(row), nil
}

func (r *domainsRepository) List(ctx context.Context) ([]*domains.Domain, error) {
	q := New(r.db)
	rows, err := q.GetAllDomains(ctx)
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
		Tracking:       row.Tracking,
	})
}
