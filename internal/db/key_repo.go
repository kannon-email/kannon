package sqlc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/values"
)

type apiKeysRepository struct {
	db *pgxpool.Pool
}

// NewAPIKeysRepository creates a new PostgreSQL-backed API keys repository
func NewAPIKeysRepository(db *pgxpool.Pool) apikeys.Repository {
	return &apiKeysRepository{db: db}
}

func (r *apiKeysRepository) Create(ctx context.Context, key *apikeys.APIKey) error {
	q := New(r.db)

	var expiresAt pgtype.Timestamp
	if key.ExpiresAt() != nil {
		expiresAt = pgtype.Timestamp{Time: *key.ExpiresAt(), Valid: true}
	}

	_, err := q.CreateAPIKey(ctx, CreateAPIKeyParams{
		ID:        key.ID().String(),
		Domain:    key.DomainName().String(),
		KeyHash:   key.KeyHash(),
		KeyPrefix: key.KeyPrefix(),
		Name:      key.Name(),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *apiKeysRepository) Update(ctx context.Context, ref apikeys.KeyRef, updateFn apikeys.UpdateFunc) (*apikeys.APIKey, error) {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}

	//nolint:errcheck
	defer tx.Rollback(ctx)

	// Create transactional queries
	txq := New(r.db).WithTx(tx)

	// Get with row lock
	row, err := txq.GetAPIKeyByIDForUpdate(ctx, GetAPIKeyByIDForUpdateParams{
		ID:     ref.KeyID().String(),
		Domain: ref.DomainName().String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	// Convert to domain model
	key, err := rowToAPIKey(row)
	if err != nil {
		return nil, err
	}

	// Apply update function
	if err := updateFn(key); err != nil {
		return nil, err
	}

	// Prepare timestamps for persistence
	var expiresAt pgtype.Timestamp
	if key.ExpiresAt() != nil {
		expiresAt = pgtype.Timestamp{Time: *key.ExpiresAt(), Valid: true}
	}

	var deactivatedAt pgtype.Timestamp
	if key.DeactivatedAt() != nil {
		deactivatedAt = pgtype.Timestamp{Time: *key.DeactivatedAt(), Valid: true}
	}

	// Persist changes
	_, err = txq.UpdateAPIKey(ctx, UpdateAPIKeyParams{
		ID:            key.ID().String(),
		Domain:        key.DomainName().String(),
		Name:          key.Name(),
		ExpiresAt:     expiresAt,
		IsActive:      key.IsActiveStatus(),
		DeactivatedAt: deactivatedAt,
	})
	if err != nil {
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return key, nil
}

func (r *apiKeysRepository) GetByKeyHash(ctx context.Context, domain values.DomainName, keyHash string) (*apikeys.APIKey, error) {
	q := New(r.db)

	// Always perform database lookup to prevent timing attacks
	row, err := q.GetAPIKeyByHash(ctx, GetAPIKeyByHashParams{
		KeyHash: keyHash,
		Domain:  domain.String(),
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	return rowToAPIKey(row)
}

func (r *apiKeysRepository) GetByID(ctx context.Context, ref apikeys.KeyRef) (*apikeys.APIKey, error) {
	q := New(r.db)

	row, err := q.GetAPIKeyByID(ctx, GetAPIKeyByIDParams{
		ID:     ref.KeyID().String(),
		Domain: ref.DomainName().String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	return rowToAPIKey(row)
}

func (r *apiKeysRepository) List(ctx context.Context, domain values.DomainName, filters apikeys.ListFilters, page apikeys.Pagination) ([]*apikeys.APIKey, error) {
	q := New(r.db)

	rows, err := q.ListAPIKeysByDomain(ctx, ListAPIKeysByDomainParams{
		Domain:  domain.String(),
		Column2: filters.OnlyActive,
		Limit:   int32(page.Limit),
		Offset:  int32(page.Offset),
	})
	if err != nil {
		return nil, err
	}

	keys := make([]*apikeys.APIKey, len(rows))
	for i, row := range rows {
		key, err := rowToAPIKey(row)
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}

	return keys, nil
}

func (r *apiKeysRepository) Count(ctx context.Context, domain values.DomainName, filters apikeys.ListFilters) (int, error) {
	q := New(r.db)

	count, err := q.CountAPIKeysByDomain(ctx, CountAPIKeysByDomainParams{
		Domain:  domain.String(),
		Column2: filters.OnlyActive,
	})
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// Helper functions to convert sqlc rows to domain model

// rowToAPIKey converts an ApiKey row to domain model
// Works with all query result types since they all use SELECT *
//
// The stored domain is canonicalised on the way in, as in the other row
// converters: a key whose domain cannot be parsed belongs to a Domain no lookup
// can name, and in an authentication path that must fail loudly rather than
// resolve to something unaddressable.
func rowToAPIKey(row ApiKey) (*apikeys.APIKey, error) {
	domain, err := values.Parse(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("api key row %q holds a non-canonical domain %q: %w", row.ID, row.Domain, err)
	}

	params := apikeys.LoadAPIKeyParams{
		ID:        apikeys.ID(row.ID),
		KeyHash:   row.KeyHash,
		KeyPrefix: row.KeyPrefix,
		Name:      row.Name,
		Domain:    domain,
		IsActive:  row.IsActive,
	}

	if row.CreatedAt.Valid {
		params.CreatedAt = row.CreatedAt.Time
	}
	if row.ExpiresAt.Valid {
		params.ExpiresAt = &row.ExpiresAt.Time
	}
	if row.DeactivatedAt.Valid {
		params.DeactivatedAt = &row.DeactivatedAt.Time
	}

	return apikeys.LoadAPIKey(params), nil
}
