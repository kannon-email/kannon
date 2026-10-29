package sqlc

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

func TestAggregatedStatsRepository(t *testing.T) {
	repo := NewAggregatedStatsRepository(db)
	stats.RunAggregatedRepoSpec(t, repo)
	stats.RunCountingRepoSpec(t, repo)
}

// A real storage failure after claiming the event must roll back that claim.
// Observe only Count and Query; the constraint is a database fault injection.
func TestCountingRetriesAfterCounterWriteFailure(t *testing.T) {
	ctx := t.Context()
	domain := values.MustParse("counting-write-failure.test")
	at := time.Now().UTC()
	id := stats.StoredEventID{Stream: domain.String(), StreamCreated: at, Sequence: 1}
	repo := NewAggregatedStatsRepository(db)
	require.NoError(t, repo.Count(ctx, id, domain, at, stats.TypeOpened))
	_, err := db.Exec(ctx, `ALTER TABLE aggregated_stats ADD CONSTRAINT counting_write_failure CHECK (domain <> 'counting-write-failure.test' OR count < 2) NOT VALID`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Exec(context.Background(), `ALTER TABLE aggregated_stats DROP CONSTRAINT IF EXISTS counting_write_failure`)
		require.NoError(t, cleanupErr)
	})
	id.Sequence = 2
	require.Error(t, repo.Count(ctx, id, domain, at, stats.TypeOpened))
	rows, err := repo.Query(ctx, domain, stats.TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0].Count)
	_, err = db.Exec(ctx, `ALTER TABLE aggregated_stats DROP CONSTRAINT counting_write_failure`)
	require.NoError(t, err)
	require.NoError(t, repo.Count(ctx, id, domain, at, stats.TypeOpened))
	require.NoError(t, repo.Count(ctx, id, domain, at, stats.TypeOpened))
	rows, err = repo.Query(ctx, domain, stats.TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(2), rows[0].Count)
}

func TestCountingSurvivesStorageReconnect(t *testing.T) {
	domain := values.MustParse("counting-reconnect.test")
	at := time.Now().UTC()
	id := stats.StoredEventID{Stream: domain.String(), StreamCreated: at, Sequence: 1}
	require.NoError(t, NewAggregatedStatsRepository(db).Count(t.Context(), id, domain, at, stats.TypeClicked))
	// A fresh connection pool and adapter have none of the original process state.
	reconnected, err := pgxpool.NewWithConfig(t.Context(), db.Config())
	require.NoError(t, err)
	defer reconnected.Close()
	restarted := NewAggregatedStatsRepository(reconnected)
	require.NoError(t, restarted.Count(t.Context(), id, domain, at, stats.TypeClicked))
	rows, err := restarted.Query(t.Context(), domain, stats.TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0].Count)
}
