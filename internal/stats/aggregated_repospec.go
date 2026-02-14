package stats

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunAggregatedRepoSpec runs the repository specification tests against any AggregatedStatsRepository implementation.
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
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	err := repo.Increment(ctx, domain, day, TypeDelivered)
	require.NoError(t, err)

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: day.Add(-24 * time.Hour),
		Stop:  day.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, TypeDelivered, results[0].Type)
	assert.Equal(t, int64(1), results[0].Count)
}

func testIncrementAccumulates(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("incr-accum-%d.test", time.Now().UnixNano())
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	for range 5 {
		err := repo.Increment(ctx, domain, day, TypeDelivered)
		require.NoError(t, err)
	}

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: day.Add(-24 * time.Hour),
		Stop:  day.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(5), results[0].Count)
}

func testIncrementSeparateEntries(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	domain := fmt.Sprintf("incr-sep-%d.test", time.Now().UnixNano())
	day1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)

	// Different day, same type
	err := repo.Increment(ctx, domain, day1, TypeDelivered)
	require.NoError(t, err)
	err = repo.Increment(ctx, domain, day2, TypeDelivered)
	require.NoError(t, err)

	// Same day, different type
	err = repo.Increment(ctx, domain, day1, TypeOpened)
	require.NoError(t, err)

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: day1.Add(-24 * time.Hour),
		Stop:  day2.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Len(t, results, 3, "different day/type combos should create separate entries")
}

func testAggregatedQueryFiltersByDomain(t *testing.T, repo AggregatedStatsRepository) {
	ctx := t.Context()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	domainA := fmt.Sprintf("agg-a-%s.test", suffix)
	domainB := fmt.Sprintf("agg-b-%s.test", suffix)
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	for range 3 {
		err := repo.Increment(ctx, domainA, day, TypeDelivered)
		require.NoError(t, err)
	}
	for range 2 {
		err := repo.Increment(ctx, domainB, day, TypeDelivered)
		require.NoError(t, err)
	}

	tr := TimeRange{Start: day.Add(-24 * time.Hour), Stop: day.Add(24 * time.Hour)}

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
	day1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2026, 1, 17, 0, 0, 0, 0, time.UTC)

	for _, day := range []time.Time{day1, day2, day3} {
		err := repo.Increment(ctx, domain, day, TypeDelivered)
		require.NoError(t, err)
	}

	// Query only day2
	results, err := repo.Query(ctx, domain, TimeRange{
		Start: day2.Add(-time.Hour),
		Stop:  day2.Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, day2, results[0].Timestamp)

	// Out of range returns empty
	results, err = repo.Query(ctx, domain, TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Stop:  time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Empty(t, results)
}
