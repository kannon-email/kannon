package sqlc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
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
		Domain:         d.Name().String(),
		DkimPrivateKey: d.DkimPrivateKey(),
		DkimPublicKey:  d.DkimPublicKey(),
	})
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "domains_domain_key" {
			return domains.ErrDomainAlreadyExists
		}
		return err
	}
	loaded, err := rowToDomain(row)
	if err != nil {
		return err
	}
	*d = *loaded
	return nil
}

func (r *domainsRepository) SetTrackingPolicy(ctx context.Context, domain values.DomainName, p tracking.Policy) (*domains.Domain, error) {
	q := New(r.db)
	row, err := q.SetDomainTracking(ctx, SetDomainTrackingParams{
		Domain:   domain.String(),
		Tracking: p.Normalized(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domains.ErrDomainNotFound
		}
		return nil, err
	}
	return rowToDomain(row)
}

func (r *domainsRepository) FindByName(ctx context.Context, domain values.DomainName) (*domains.Domain, error) {
	q := New(r.db)
	row, err := q.FindDomain(ctx, domain.String())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domains.ErrDomainNotFound
		}
		return nil, err
	}
	return rowToDomain(row)
}

func (r *domainsRepository) List(ctx context.Context) ([]*domains.Domain, error) {
	q := New(r.db)
	rows, err := q.GetAllDomains(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*domains.Domain, 0, len(rows))
	for _, row := range rows {
		d, err := rowToDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// rowToDomain rebuilds the entity from its row. The stored name goes back through
// values.Parse rather than being trusted: a row predating the canonical form must not become
// a Domain whose name no Grant and no query can match, so the data fault is named instead.
func rowToDomain(row Domain) (*domains.Domain, error) {
	name, err := values.Parse(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("domain row %q holds a non-canonical name: %w", row.Domain, err)
	}
	return domains.Load(domains.LoadParams{
		ID:             row.ID,
		Domain:         name,
		DkimPrivateKey: row.DkimPrivateKey,
		DkimPublicKey:  row.DkimPublicKey,
		CreatedAt:      row.CreatedAt.Time,
		// Normalised on the way out, so a Domain always states a ceiling on both axes. A ceiling
		// that states nothing enforces nothing (ADR 0003), and that invariant should rest on one
		// enforcement point rather than on the column default and the write path both holding.
		Tracking: row.Tracking.Normalized(),
	}), nil
}

// ListPage uses the unique Domain name as an exclusive keyset boundary.
func (r *domainsRepository) ListPage(ctx context.Context, after string, limit int) ([]*domains.Domain, error) {
	rows, err := r.db.Query(ctx, `SELECT id, domain, dkim_private_key, dkim_public_key, created_at, tracking FROM domains WHERE domain > $1 ORDER BY domain LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domains.Domain{}
	for rows.Next() {
		var row Domain
		if err := rows.Scan(&row.ID, &row.Domain, &row.DkimPrivateKey, &row.DkimPublicKey, &row.CreatedAt, &row.Tracking); err != nil {
			return nil, err
		}
		d, err := rowToDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
