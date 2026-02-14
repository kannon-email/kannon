package stats

import (
	"fmt"
	"testing"
	"time"

	pbtypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	deliveredData = &pbtypes.StatsData{Data: &pbtypes.StatsData_Delivered{Delivered: &pbtypes.StatsDataDelivered{}}}
	openedData    = &pbtypes.StatsData{Data: &pbtypes.StatsData_Opened{Opened: &pbtypes.StatsDataOpened{}}}
)

// RunRepoSpec runs the repository specification tests against any Repository implementation.
func RunRepoSpec(t *testing.T, repo Repository) {
	t.Run("Insert+Query", func(t *testing.T) {
		testInsertAndQuery(t, repo)
	})
	t.Run("Query/Pagination", func(t *testing.T) {
		testQueryPagination(t, repo)
	})
	t.Run("Query/FiltersByDomain", func(t *testing.T) {
		testQueryFiltersByDomain(t, repo)
	})
	t.Run("Query/FiltersByTimeRange", func(t *testing.T) {
		testQueryFiltersByTimeRange(t, repo)
	})
	t.Run("Count", func(t *testing.T) {
		testCount(t, repo)
	})
	t.Run("QueryTimeline", func(t *testing.T) {
		testQueryTimeline(t, repo)
	})
	t.Run("DeleteOlderThan", func(t *testing.T) {
		testDeleteOlderThan(t, repo)
	})
}

func testInsertAndQuery(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("insert-query-%d.test", time.Now().UnixNano())
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	stat := &Stat{
		Type:      TypeDelivered,
		Email:     "user@example.com",
		MessageID: "msg-001",
		Domain:    domain,
		Timestamp: now,
		Data:      deliveredData,
	}

	err := repo.Insert(ctx, stat)
	require.NoError(t, err)

	results, err := repo.Query(ctx, domain, TimeRange{
		Start: now.Add(-time.Hour),
		Stop:  now.Add(time.Hour),
	}, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, TypeDelivered, results[0].Type)
	assert.Equal(t, "user@example.com", results[0].Email)
	assert.Equal(t, "msg-001", results[0].MessageID)
	assert.Equal(t, domain, results[0].Domain)
}

func testQueryPagination(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("pagination-%d.test", time.Now().UnixNano())
	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	for i := range 5 {
		err := repo.Insert(ctx, &Stat{
			Type:      TypeDelivered,
			Email:     fmt.Sprintf("user%d@example.com", i),
			MessageID: fmt.Sprintf("msg-%d", i),
			Domain:    domain,
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Data:      deliveredData,
		})
		require.NoError(t, err)
	}

	tr := TimeRange{Start: base.Add(-time.Hour), Stop: base.Add(time.Hour)}

	page1, err := repo.Query(ctx, domain, tr, Pagination{Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	page2, err := repo.Query(ctx, domain, tr, Pagination{Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	page3, err := repo.Query(ctx, domain, tr, Pagination{Limit: 2, Offset: 4})
	require.NoError(t, err)
	assert.Len(t, page3, 1)
}

func testQueryFiltersByDomain(t *testing.T, repo Repository) {
	ctx := t.Context()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	domainA := fmt.Sprintf("domain-a-%s.test", suffix)
	domainB := fmt.Sprintf("domain-b-%s.test", suffix)
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	for i := range 3 {
		err := repo.Insert(ctx, &Stat{
			Type: TypeDelivered, Email: fmt.Sprintf("a%d@example.com", i),
			MessageID: fmt.Sprintf("a-msg-%d", i), Domain: domainA, Timestamp: now,
			Data: deliveredData,
		})
		require.NoError(t, err)
	}
	for i := range 2 {
		err := repo.Insert(ctx, &Stat{
			Type: TypeOpened, Email: fmt.Sprintf("b%d@example.com", i),
			MessageID: fmt.Sprintf("b-msg-%d", i), Domain: domainB, Timestamp: now,
			Data: openedData,
		})
		require.NoError(t, err)
	}

	tr := TimeRange{Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}

	resultsA, err := repo.Query(ctx, domainA, tr, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, resultsA, 3)

	resultsB, err := repo.Query(ctx, domainB, tr, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, resultsB, 2)
}

func testQueryFiltersByTimeRange(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("timerange-%d.test", time.Now().UnixNano())
	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	// Insert stats at base+0m, base+30m, base+60m
	for i, offset := range []time.Duration{0, 30 * time.Minute, 60 * time.Minute} {
		err := repo.Insert(ctx, &Stat{
			Type: TypeDelivered, Email: fmt.Sprintf("u%d@example.com", i),
			MessageID: fmt.Sprintf("msg-%d", i), Domain: domain, Timestamp: base.Add(offset),
			Data: deliveredData,
		})
		require.NoError(t, err)
	}

	// [base, base+60m) should include first two (start inclusive, stop exclusive)
	results, err := repo.Query(ctx, domain, TimeRange{
		Start: base,
		Stop:  base.Add(60 * time.Minute),
	}, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, results, 2, "stop should be exclusive")

	// [base, base+61m) should include all three
	results, err = repo.Query(ctx, domain, TimeRange{
		Start: base,
		Stop:  base.Add(61 * time.Minute),
	}, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func testCount(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("count-%d.test", time.Now().UnixNano())
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	for i := range 4 {
		err := repo.Insert(ctx, &Stat{
			Type: TypeDelivered, Email: fmt.Sprintf("u%d@example.com", i),
			MessageID: fmt.Sprintf("msg-%d", i), Domain: domain, Timestamp: now,
			Data: deliveredData,
		})
		require.NoError(t, err)
	}

	tr := TimeRange{Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}
	count, err := repo.Count(ctx, domain, tr)
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)
}

func testQueryTimeline(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("timeline-%d.test", time.Now().UnixNano())
	hour1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	hour2 := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)

	// 2 delivered in hour1, 1 opened in hour1, 1 delivered in hour2
	for _, s := range []*Stat{
		{Type: TypeDelivered, Email: "a@e.com", MessageID: "m1", Domain: domain, Timestamp: hour1.Add(5 * time.Minute), Data: deliveredData},
		{Type: TypeDelivered, Email: "b@e.com", MessageID: "m2", Domain: domain, Timestamp: hour1.Add(15 * time.Minute), Data: deliveredData},
		{Type: TypeOpened, Email: "c@e.com", MessageID: "m3", Domain: domain, Timestamp: hour1.Add(20 * time.Minute), Data: openedData},
		{Type: TypeDelivered, Email: "d@e.com", MessageID: "m4", Domain: domain, Timestamp: hour2.Add(10 * time.Minute), Data: deliveredData},
	} {
		err := repo.Insert(ctx, s)
		require.NoError(t, err)
	}

	tr := TimeRange{Start: hour1, Stop: hour2.Add(time.Hour)}
	timeline, err := repo.QueryTimeline(ctx, domain, tr)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(timeline), 3)

	// Verify sorted by timestamp
	for i := 1; i < len(timeline); i++ {
		assert.False(t, timeline[i].Timestamp.Before(timeline[i-1].Timestamp),
			"timeline should be sorted by timestamp")
	}

	// Verify hourly bucketing: find hour1/delivered bucket
	var hour1Delivered *AggregatedStat
	for _, a := range timeline {
		if a.Timestamp.Equal(hour1) && a.Type == TypeDelivered {
			hour1Delivered = a
		}
	}
	require.NotNil(t, hour1Delivered, "should have hour1/delivered bucket")
	assert.Equal(t, int64(2), hour1Delivered.Count)
}

func testDeleteOlderThan(t *testing.T, repo Repository) {
	ctx := t.Context()
	domain := fmt.Sprintf("delete-%d.test", time.Now().UnixNano())
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	err := repo.Insert(ctx, &Stat{
		Type: TypeDelivered, Email: "old@e.com", MessageID: "old-1",
		Domain: domain, Timestamp: old, Data: deliveredData,
	})
	require.NoError(t, err)

	err = repo.Insert(ctx, &Stat{
		Type: TypeDelivered, Email: "recent@e.com", MessageID: "recent-1",
		Domain: domain, Timestamp: recent, Data: deliveredData,
	})
	require.NoError(t, err)

	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	deleted, err := repo.DeleteOlderThan(ctx, cutoff)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(1))

	// Recent stat should still be there
	tr := TimeRange{Start: recent.Add(-time.Hour), Stop: recent.Add(time.Hour)}
	remaining, err := repo.Query(ctx, domain, tr, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "recent@e.com", remaining[0].Email)

	// Old stat should be gone
	oldTr := TimeRange{Start: old.Add(-time.Hour), Stop: old.Add(time.Hour)}
	oldResults, err := repo.Query(ctx, domain, oldTr, Pagination{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Empty(t, oldResults)
}
