package stats

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *sq.Queries

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sq.New(db)

	code := m.Run()

	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

const testRetention = 365 * 24 * time.Hour

func newTestHandler() statsHandler {
	repo := sq.NewStatsRepository(q)
	service := stats.NewService(repo)
	return statsHandler{
		service:   service,
		q:         q,
		retention: testRetention,
	}
}

func TestCleanupCycle_DeletesOldStats(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()

	// Insert an old stat (2 years ago)
	oldTime := time.Now().Add(-2 * 365 * 24 * time.Hour)
	err := q.InsertStat(ctx, sq.InsertStatParams{
		Email:     "old@test.com",
		MessageID: "msg-old",
		Timestamp: pgtype.Timestamp{Time: oldTime, Valid: true},
		Domain:    "test.com",
		Type:      sq.StatsTypeDelivered,
		Data:      &types.StatsData{Data: &types.StatsData_Accepted{Accepted: &types.StatsDataAccepted{}}},
	})
	require.NoError(t, err)

	// Insert a recent stat (1 hour ago)
	recentTime := time.Now().Add(-1 * time.Hour)
	err = q.InsertStat(ctx, sq.InsertStatParams{
		Email:     "recent@test.com",
		MessageID: "msg-recent",
		Timestamp: pgtype.Timestamp{Time: recentTime, Valid: true},
		Domain:    "test.com",
		Type:      sq.StatsTypeDelivered,
		Data:      &types.StatsData{Data: &types.StatsData_Accepted{Accepted: &types.StatsDataAccepted{}}},
	})
	require.NoError(t, err)

	h := newTestHandler() // 1 year

	err = runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	// Only the recent stat should remain
	rows, err := q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: "test.com",
		Start:  pgtype.Timestamp{Time: oldTime.Add(-time.Hour), Valid: true},
		Stop:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestCleanupCycle_KeepsRecentStats(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()

	// Insert two recent stats
	for _, email := range []string{"a@test.com", "b@test.com"} {
		err := q.InsertStat(ctx, sq.InsertStatParams{
			Email:     email,
			MessageID: "msg-" + email,
			Timestamp: pgtype.Timestamp{Time: time.Now().Add(-1 * time.Hour), Valid: true},
			Domain:    "test.com",
			Type:      sq.StatsTypeDelivered,
			Data:      &types.StatsData{Data: &types.StatsData_Accepted{Accepted: &types.StatsDataAccepted{}}},
		})
		require.NoError(t, err)
	}

	h := newTestHandler()

	err := runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	rows, err := q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: "test.com",
		Start:  pgtype.Timestamp{Time: time.Now().Add(-2 * time.Hour), Valid: true},
		Stop:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
}

func TestCleanupCycle_DeletesExpiredStatsKeys(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()

	// Insert an expired key
	_, err := q.CreateStatsKeys(ctx, sq.CreateStatsKeysParams{
		ID:         "expired-key",
		PrivateKey: "priv",
		PublicKey:  "pub",
		ExpirationTime: pgtype.Timestamp{
			Time:  time.Now().Add(-24 * time.Hour),
			Valid: true,
		},
	})
	require.NoError(t, err)

	// Insert a valid key
	_, err = q.CreateStatsKeys(ctx, sq.CreateStatsKeysParams{
		ID:         "valid-key",
		PrivateKey: "priv2",
		PublicKey:  "pub2",
		ExpirationTime: pgtype.Timestamp{
			Time:  time.Now().Add(24 * time.Hour),
			Valid: true,
		},
	})
	require.NoError(t, err)

	h := newTestHandler()

	err = runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	// Valid key should still exist
	var validCount int
	err = db.QueryRow(ctx, "SELECT COUNT(*) FROM stats_keys WHERE id = $1", "valid-key").Scan(&validCount)
	require.NoError(t, err)
	assert.Equal(t, 1, validCount)

	// Expired key should be deleted from the table
	var expiredCount int
	err = db.QueryRow(ctx, "SELECT COUNT(*) FROM stats_keys WHERE id = $1", "expired-key").Scan(&expiredCount)
	require.NoError(t, err)
	assert.Equal(t, 0, expiredCount)
}

func TestCleanupCycle_NoRowsToDelete(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()

	h := newTestHandler()

	// Should not error on empty tables
	err := runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	assert.NoError(t, err)
}

func cleanDB(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, "DELETE FROM stats")
	require.NoError(t, err)
	_, err = db.Exec(ctx, "DELETE FROM stats_keys")
	require.NoError(t, err)
}
