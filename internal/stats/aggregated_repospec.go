package stats

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunAggregatedRepoSpec runs the repository specification tests against any AggregatedStatsRepository implementation.
//
// Buckets are the hourly ones the service writes: the repository stores the
// timestamp it is handed, so the spec keeps every fixture on an hour boundary and
// checks that adjacent hours stay distinct rows.
func RunAggregatedRepoSpec(t *testing.T, repo AggregatedStatsRepository) {
	t.Run("Increment+Query", func(t *testing.T) {
		testIncrementAndQuery(t, repo)
	})
	t.Run("Increment/Accumulates", func(t *testing.T) {
		testIncrementAccumulates(t, repo)
	})
	t.Run("Increment/SeparateEntries", func(t *testing.T) {
		testIncrementSeparateEntries(t, repo)
	})
	t.Run("Query/FiltersByDomain", func(t *testing.T) {
		testAggregatedQueryFiltersByDomain(t, repo)
	})
	t.Run("Query/FiltersByTimeRange", func(t *testing.T) {
		testAggregatedQueryFiltersByTimeRange(t, repo)
	})
}

func testIncrementAndQuery(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("incr-query-%d.test", time.Now().UnixNano())
	hour := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	err := repo.Increment(ctx, domain, hour, TypeDelivered)
	require.NoError(t, err)

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: hour.Add(-time.Hour),
		Stop:  hour.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, TypeDelivered, results[0].Type)
	assert.Equal(t, int64(1), results[0].Count)
}

func testIncrementAccumulates(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("incr-accum-%d.test", time.Now().UnixNano())
	hour := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	for range 5 {
		err := repo.Increment(ctx, domain, hour, TypeDelivered)
		require.NoError(t, err)
	}

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: hour.Add(-time.Hour),
		Stop:  hour.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(5), results[0].Count)
}

func testIncrementSeparateEntries(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("incr-sep-%d.test", time.Now().UnixNano())
	hour1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	hour2 := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)

	// Different hour of the same day, same type
	err := repo.Increment(ctx, domain, hour1, TypeDelivered)
	require.NoError(t, err)
	err = repo.Increment(ctx, domain, hour2, TypeDelivered)
	require.NoError(t, err)

	// Same hour, different type
	err = repo.Increment(ctx, domain, hour1, TypeOpened)
	require.NoError(t, err)

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: hour1.Add(-time.Hour),
		Stop:  hour2.Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Len(t, results, 3, "different hour/type combos should create separate entries")
}

func testAggregatedQueryFiltersByDomain(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	domainA := fmt.Sprintf("agg-a-%s.test", suffix)
	domainB := fmt.Sprintf("agg-b-%s.test", suffix)
	hour := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	for range 3 {
		err := repo.Increment(ctx, domainA, hour, TypeDelivered)
		require.NoError(t, err)
	}
	for range 2 {
		err := repo.Increment(ctx, domainB, hour, TypeDelivered)
		require.NoError(t, err)
	}

	tr := TimeRange{Start: hour.Add(-time.Hour), Stop: hour.Add(time.Hour)}

	resultsA, err := repo.Query(ctx, domainA, tr)
	require.NoError(t, err)
	require.Len(t, resultsA, 1)
	assert.Equal(t, int64(3), resultsA[0].Count)

	resultsB, err := repo.Query(ctx, domainB, tr)
	require.NoError(t, err)
	require.Len(t, resultsB, 1)
	assert.Equal(t, int64(2), resultsB[0].Count)
}

func testAggregatedQueryFiltersByTimeRange(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("agg-tr-%d.test", time.Now().UnixNano())
	hour1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	hour2 := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	hour3 := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	for _, hour := range []time.Time{hour1, hour2, hour3} {
		err := repo.Increment(ctx, domain, hour, TypeDelivered)
		require.NoError(t, err)
	}

	// Query only hour2
	results, err := repo.Query(ctx, domain, TimeRange{
		Start: hour2,
		Stop:  hour3,
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, hour2, results[0].Timestamp)

	// Out of range returns empty
	results, err = repo.Query(ctx, domain, TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Stop:  time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Empty(t, results)
}
