package stats

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

// RunCountingRepoSpec verifies the count-once contract shared by storage adapters.
func RunCountingRepoSpec(t *testing.T, repo interface {
	AggregatedEventCounter
	Query(ctx context.Context, domain values.DomainName, timeRange TimeRange) ([]*AggregatedStat, error)
}) {
	t.Run("RedeliveryCountsOnce", func(t *testing.T) {
		domain := values.MustParse(fmt.Sprintf("count-%d.test", time.Now().UnixNano()))
		at := time.Date(2026, 10, 1, 10, 23, 0, 0, time.UTC)
		id := StoredEventID{Stream: domain.String(), StoredAt: at.Add(-time.Hour), Sequence: 1}
		for range 2 {
			require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeOpened))
		}
		rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int64(1), rows[0].Count)
		require.Equal(t, at.Truncate(time.Hour), rows[0].Timestamp)
	})
	t.Run("DistinctStoredEventsCountIndependently", func(t *testing.T) {
		for _, kind := range []Type{TypeOpened, TypeClicked} {
			t.Run(string(kind), func(t *testing.T) {
				domain := values.MustParse(fmt.Sprintf("distinct-%d.test", time.Now().UnixNano()))
				at := time.Date(2026, 10, 1, 10, 23, 0, 0, time.UTC)
				id := StoredEventID{Stream: domain.String(), StoredAt: at, Sequence: 1}
				require.NoError(t, repo.Count(t.Context(), id, domain, at, kind))
				id.Sequence = 2
				require.NoError(t, repo.Count(t.Context(), id, domain, at, kind))
				rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, int64(2), rows[0].Count)
			})
		}
	})
	t.Run("StoredMessageIdentitySeparatesSequenceReuse", func(t *testing.T) {
		domain := values.MustParse(fmt.Sprintf("recreated-%d.test", time.Now().UnixNano()))
		at := time.Date(2026, 10, 1, 10, 23, 0, 123456789, time.UTC)
		id := StoredEventID{Stream: domain.String(), StoredAt: at, Sequence: ^uint64(0)}
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeOpened))
		// An equivalent instant must deduplicate even with a different location.
		id.StoredAt = at.In(time.FixedZone("offset", 3600))
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeOpened))
		id.StoredAt = at.Add(time.Nanosecond)
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeOpened))
		id.Stream += "-other"
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeOpened))
		rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int64(3), rows[0].Count)
	})
	t.Run("ConcurrentRedeliveryCountsOnce", func(t *testing.T) {
		domain := values.MustParse(fmt.Sprintf("concurrent-%d.test", time.Now().UnixNano()))
		at := time.Now().UTC()
		id := StoredEventID{Stream: domain.String(), StoredAt: at, Sequence: 1}
		var wg sync.WaitGroup
		errs := make(chan error, 32)
		for range 32 {
			wg.Go(func() { errs <- repo.Count(t.Context(), id, domain, at, TypeOpened) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int64(1), rows[0].Count)
	})
	t.Run("FailedWriteCanBeRetried", func(t *testing.T) {
		domain := values.MustParse(fmt.Sprintf("retry-%d.test", time.Now().UnixNano()))
		at := time.Now().UTC()
		id := StoredEventID{Stream: domain.String(), StoredAt: at, Sequence: 1}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.Error(t, repo.Count(ctx, id, domain, at, TypeClicked))
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeClicked))
		require.NoError(t, repo.Count(t.Context(), id, domain, at, TypeClicked))
		rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int64(1), rows[0].Count)
	})
	t.Run("MissingIdentityCannotCount", func(t *testing.T) {
		domain := values.MustParse(fmt.Sprintf("invalid-%d.test", time.Now().UnixNano()))
		at := time.Now().UTC()
		for _, id := range []StoredEventID{
			{StoredAt: at, Sequence: 1},
			{Stream: "stats", Sequence: 1},
			{Stream: "stats", StoredAt: at},
		} {
			require.ErrorIs(t, repo.Count(t.Context(), id, domain, at, TypeOpened), ErrInvalidStoredEventID)
		}
		rows, err := repo.Query(t.Context(), domain, TimeRange{Start: at.Truncate(time.Hour), Stop: at.Add(time.Hour)})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}
