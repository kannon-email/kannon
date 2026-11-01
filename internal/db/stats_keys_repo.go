package sqlc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/statssec"
)

type statsKeysRepository struct {
	db DBTX
}

// NewStatsKeysRepository creates the PostgreSQL-backed store of tracking key pairs.
//
// stats_keys holds timestamps without a time zone and compares them against NOW(), which the
// database renders in UTC. Every instant is therefore written and read as UTC, so whether a key has
// expired does not depend on the time zone of the process that minted it.
func NewStatsKeysRepository(db *pgxpool.Pool) statssec.KeyRepository {
	return &statsKeysRepository{db: db}
}

func (r *statsKeysRepository) SigningKey(ctx context.Context, validUntil time.Time) (statssec.Key, error) {
	row, err := New(r.db).GetValidStatsKeys(ctx, PgTimestampFromTime(validUntil.UTC()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return statssec.Key{}, statssec.ErrNoSigningKey
		}
		return statssec.Key{}, err
	}
	return statssec.Key{
		ID:         row.ID,
		PrivateKey: row.PrivateKey,
		PublicKey:  row.PublicKey,
		ExpiresAt:  asUTC(row.ExpirationTime.Time),
	}, nil
}

func (r *statsKeysRepository) Create(ctx context.Context, k statssec.Key) error {
	_, err := New(r.db).CreateStatsKeys(ctx, CreateStatsKeysParams{
		ID:             k.ID,
		PrivateKey:     k.PrivateKey,
		PublicKey:      k.PublicKey,
		ExpirationTime: PgTimestampFromTime(k.ExpiresAt.UTC()),
	})
	return err
}

func (r *statsKeysRepository) PublicKey(ctx context.Context, id string) (string, error) {
	row, err := New(r.db).GetValidPublicStatsKeyByKid(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", statssec.ErrKeyNotFound
		}
		return "", err
	}
	return row.PublicKey, nil
}

func (r *statsKeysRepository) DeleteExpired(ctx context.Context) (int64, error) {
	return New(r.db).DeleteExpiredStatsKeys(ctx)
}

// asUTC reads a zone-less timestamp back as the UTC instant it was written as.
func asUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}
