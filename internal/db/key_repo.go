package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/apikeys"
)

type apiKeysRepository struct {
	q    *Queries
	pool *pgxpool.Pool
}

// NewAPIKeysRepository creates a new PostgreSQL-backed API keys repository
func NewAPIKeysRepository(q *Queries, pool *pgxpool.Pool) apikeys.Repository {
	return &apiKeysRepository{q: q, pool: pool}
}

func (r *apiKeysRepository) Create(ctx context.Context, key *apikeys.APIKey) error {
	var expiresAt pgtype.Timestamp
	if key.ExpiresAt() != nil {
		expiresAt = pgtype.Timestamp{Time: *key.ExpiresAt(), Valid: true}
	}

	_, err := r.q.CreateAPIKey(ctx, CreateAPIKeyParams{
		ID:        key.ID().String(),
		Domain:    key.Domain(),
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
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}

	//nolint:errcheck
	defer tx.Rollback(ctx)

	// Create transactional queries
	txq := r.q.WithTx(tx)

	// Get with row lock
	row, err := txq.GetAPIKeyByIDForUpdate(ctx, GetAPIKeyByIDForUpdateParams{
		ID:     ref.KeyID().String(),
		Domain: ref.Domain(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	// Convert to domain model
	key := rowToAPIKey(row)

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
		Domain:        key.Domain(),
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

func (r *apiKeysRepository) GetByKeyHash(ctx context.Context, domain, keyHash string) (*apikeys.APIKey, error) {
	// Always perform database lookup to prevent timing attacks
	row, err := r.q.GetAPIKeyByHash(ctx, GetAPIKeyByHashParams{
		KeyHash: keyHash,
		Domain:  domain,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	return rowToAPIKey(row), nil
}

func (r *apiKeysRepository) GetByID(ctx context.Context, ref apikeys.KeyRef) (*apikeys.APIKey, error) {
	row, err := r.q.GetAPIKeyByID(ctx, GetAPIKeyByIDParams{
		ID:     ref.KeyID().String(),
		Domain: ref.Domain(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apikeys.ErrKeyNotFound
		}
		return nil, err
	}

	return rowToAPIKey(row), nil
}

func (r *apiKeysRepository) List(ctx context.Context, domain string, filters apikeys.ListFilters, page apikeys.Pagination) ([]*apikeys.APIKey, error) {
	rows, err := r.q.ListAPIKeysByDomain(ctx, ListAPIKeysByDomainParams{
		Domain:  domain,
		Column2: filters.OnlyActive,
		Limit:   int32(page.Limit),
		Offset:  int32(page.Offset),
	})
	if err != nil {
		return nil, err
	}

	keys := make([]*apikeys.APIKey, len(rows))
	for i, row := range rows {
		keys[i] = rowToAPIKey(row)
	}

	return keys, nil
}

func (r *apiKeysRepository) Count(ctx context.Context, domain string, filters apikeys.ListFilters) (int, error) {
	count, err := r.q.CountAPIKeysByDomain(ctx, CountAPIKeysByDomainParams{
		Domain:  domain,
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
func rowToAPIKey(row ApiKey) *apikeys.APIKey {
	params := apikeys.LoadAPIKeyParams{
		ID:        apikeys.ID(row.ID),
		KeyHash:   row.KeyHash,
		KeyPrefix: row.KeyPrefix,
		Name:      row.Name,
		Domain:    row.Domain,
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

	return apikeys.LoadAPIKey(params)
}
