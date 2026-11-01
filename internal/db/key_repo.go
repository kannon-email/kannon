package sqlc

import (
	"context"
	"errors"
	"fmt"
	"github.com/kannon-email/kannon/internal/authz"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/keyset"
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

	var expiresAt pgtype.Timestamptz
	if key.ExpiresAt() != nil {
		expiresAt = pgtype.Timestamptz{Time: *key.ExpiresAt(), Valid: true}
	}

	_, err := q.CreateAPIKey(ctx, CreateAPIKeyParams{
		ID:        key.ID().String(),
		Domain:    key.DomainName().String(),
		KeyHash:   key.KeyHash(),
		KeyPrefix: key.KeyPrefix(),
		Name:      key.Name(),
		ExpiresAt: expiresAt,
		Roles:     roleStrings(key.Roles()),
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *apiKeysRepository) Update(ctx context.Context, ref apikeys.KeyRef, updateFn apikeys.UpdateFunc) (*apikeys.APIKey, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}

	//nolint:errcheck
	defer tx.Rollback(ctx)

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

	key, err := rowToAPIKey(row)
	if err != nil {
		return nil, err
	}

	if err := updateFn(key); err != nil {
		return nil, err
	}

	var expiresAt pgtype.Timestamptz
	if key.ExpiresAt() != nil {
		expiresAt = pgtype.Timestamptz{Time: *key.ExpiresAt(), Valid: true}
	}

	var deactivatedAt pgtype.Timestamptz
	if key.DeactivatedAt() != nil {
		deactivatedAt = pgtype.Timestamptz{Time: *key.DeactivatedAt(), Valid: true}
	}

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
	return rowsToAPIKeys(rows)
}

func (r *apiKeysRepository) ListPage(ctx context.Context, domain values.DomainName, page keyset.Page[apikeys.Position]) ([]*apikeys.APIKey, error) {
	params := ListAPIKeysPageParams{Domain: domain.String(), Take: int32(page.Limit)}
	if page.After != nil {
		params.BeforeTime = pgtype.Timestamptz{Time: page.After.CreatedAt, Valid: true}
		params.BeforeID = page.After.ID
	}
	rows, err := New(r.db).ListAPIKeysPage(ctx, params)
	if err != nil {
		return nil, err
	}
	return rowsToAPIKeys(rows)
}

func rowsToAPIKeys(rows []ApiKey) ([]*apikeys.APIKey, error) {
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

// rowToAPIKey converts an ApiKey row to the domain model, and works with every query result type
// since they all use SELECT *. The stored domain is canonicalised on the way in: a key whose
// domain cannot be parsed belongs to a Domain no lookup can name, which must fail loudly.
func rowToAPIKey(row ApiKey) (*apikeys.APIKey, error) {
	domain, err := values.Parse(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("api key row %q holds a non-canonical domain %q: %w", row.ID, row.Domain, err)
	}

	roles := []authz.RoleName{}
	for _, raw := range row.Roles {
		role, err := authz.ParseRoleName(raw)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	params := apikeys.LoadAPIKeyParams{
		Roles:     roles,
		ID:        apikeys.ID(row.ID),
		KeyHash:   row.KeyHash,
		KeyPrefix: row.KeyPrefix,
		Name:      row.Name,
		Domain:    domain,
		IsActive:  row.IsActive,
	}

	// pgx decodes timestamptz in the process's local zone; keys are loaded in UTC
	// whatever zone they were stored from, as they were before the columns held
	// a zone.
	if row.CreatedAt.Valid {
		params.CreatedAt = row.CreatedAt.Time.UTC()
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time.UTC()
		params.ExpiresAt = &expiresAt
	}
	if row.DeactivatedAt.Valid {
		deactivatedAt := row.DeactivatedAt.Time.UTC()
		params.DeactivatedAt = &deactivatedAt
	}

	return apikeys.LoadAPIKey(params), nil
}

func roleStrings(roles []authz.RoleName) []string {
	result := make([]string, len(roles))
	for i, r := range roles {
		result[i] = string(r)
	}
	return result
}

// FindByKeyHash resolves a Bearer credential independently of its request target.
func (r *apiKeysRepository) FindByKeyHash(ctx context.Context, hash string) (*apikeys.APIKey, error) {
	row, err := New(r.db).FindAPIKeyByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apikeys.ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToAPIKey(row)
}
