package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
)

func newTestService() *stats.Service {
	return stats.NewService(stats.NewInMemRepository())
}

func TestInsertAndQueryStats(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	tr := stats.TimeRange{Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}

	stat := stats.NewStat("user@example.com", "msg-1", "example.com", now, &types.StatsData{
		Data: &types.StatsData_Delivered{},
	})

	if err := svc.InsertStat(ctx, stat); err != nil {
		t.Fatalf("InsertStat: %v", err)
	}

	results, total, err := svc.QueryStats(ctx, "example.com", tr, stats.Pagination{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}

	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Type != stats.TypeDelivered {
		t.Errorf("expected type %s, got %s", stats.TypeDelivered, results[0].Type)
	}
	if results[0].Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", results[0].Email)
	}
}

func TestQueryStats_Pagination(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	tr := stats.TimeRange{Start: base.Add(-time.Hour), Stop: base.Add(time.Hour)}

	for i := 0; i < 5; i++ {
		s := stats.NewStat("user@example.com", "msg-1", "example.com", base.Add(time.Duration(i)*time.Minute), &types.StatsData{
			Data: &types.StatsData_Delivered{},
		})
		if err := svc.InsertStat(ctx, s); err != nil {
			t.Fatalf("InsertStat: %v", err)
		}
	}

	results, total, err := svc.QueryStats(ctx, "example.com", tr, stats.Pagination{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	results, _, err = svc.QueryStats(ctx, "example.com", tr, stats.Pagination{Limit: 10, Offset: 4})
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result at offset 4, got %d", len(results))
	}
}

func TestQueryStats_FiltersByDomain(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	tr := stats.TimeRange{Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}

	for _, domain := range []string{"a.com", "b.com", "a.com"} {
		s := stats.NewStat("u@"+domain, "msg", domain, now, &types.StatsData{
			Data: &types.StatsData_Accepted{},
		})
		if err := svc.InsertStat(ctx, s); err != nil {
			t.Fatalf("InsertStat: %v", err)
		}
	}

	results, total, err := svc.QueryStats(ctx, "a.com", tr, stats.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for a.com, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for a.com, got %d", len(results))
	}
}

func TestQueryStats_FiltersByTimeRange(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	base := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tr := stats.TimeRange{Start: base, Stop: base.Add(2 * time.Hour)}

	times := []time.Time{
		base.Add(-time.Minute), // before range
		base,                   // at start (included)
		base.Add(time.Hour),    // in range
		base.Add(2 * time.Hour), // at stop (excluded)
	}

	for _, ts := range times {
		s := stats.NewStat("u@d.com", "msg", "d.com", ts, &types.StatsData{
			Data: &types.StatsData_Opened{},
		})
		if err := svc.InsertStat(ctx, s); err != nil {
			t.Fatalf("InsertStat: %v", err)
		}
	}

	_, total, err := svc.QueryStats(ctx, "d.com", tr, stats.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 stats in range [start, stop), got %d", total)
	}
}

func TestQueryTimeline(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	tr := stats.TimeRange{Start: base, Stop: base.Add(3 * time.Hour)}

	// 2 delivered in hour 10, 1 delivered + 1 opened in hour 11.
	inserts := []struct {
		ts   time.Time
		data *types.StatsData
	}{
		{base.Add(5 * time.Minute), &types.StatsData{Data: &types.StatsData_Delivered{}}},
		{base.Add(30 * time.Minute), &types.StatsData{Data: &types.StatsData_Delivered{}}},
		{base.Add(65 * time.Minute), &types.StatsData{Data: &types.StatsData_Delivered{}}},
		{base.Add(90 * time.Minute), &types.StatsData{Data: &types.StatsData_Opened{}}},
	}

	for _, ins := range inserts {
		s := stats.NewStat("u@d.com", "msg", "d.com", ins.ts, ins.data)
		if err := svc.InsertStat(ctx, s); err != nil {
			t.Fatalf("InsertStat: %v", err)
		}
	}

	timeline, err := svc.QueryTimeline(ctx, "d.com", tr)
	if err != nil {
		t.Fatalf("QueryTimeline: %v", err)
	}

	if len(timeline) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(timeline))
	}

	// Sorted by timestamp then type: hour10/delivered, hour11/delivered, hour11/opened.
	assertBucket(t, timeline[0], stats.TypeDelivered, base, 2)
	assertBucket(t, timeline[1], stats.TypeDelivered, base.Add(time.Hour), 1)
	assertBucket(t, timeline[2], stats.TypeOpened, base.Add(time.Hour), 1)
}

func TestCleanup(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)

	for _, ts := range []time.Time{old, old, recent} {
		s := stats.NewStat("u@d.com", "msg", "d.com", ts, &types.StatsData{
			Data: &types.StatsData_Delivered{},
		})
		if err := svc.InsertStat(ctx, s); err != nil {
			t.Fatalf("InsertStat: %v", err)
		}
	}

	deleted, err := svc.Cleanup(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	tr := stats.TimeRange{Start: now.Add(-72 * time.Hour), Stop: now.Add(time.Hour)}
	_, total, err := svc.QueryStats(ctx, "d.com", tr, stats.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("QueryStats after cleanup: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 remaining, got %d", total)
	}
}

func TestDetermineType(t *testing.T) {
	tests := []struct {
		name string
		data *types.StatsData
		want stats.Type
	}{
		{"nil", nil, stats.TypeUnknown},
		{"accepted", &types.StatsData{Data: &types.StatsData_Accepted{}}, stats.TypeAccepted},
		{"rejected", &types.StatsData{Data: &types.StatsData_Rejected{}}, stats.TypeRejected},
		{"delivered", &types.StatsData{Data: &types.StatsData_Delivered{}}, stats.TypeDelivered},
		{"opened", &types.StatsData{Data: &types.StatsData_Opened{}}, stats.TypeOpened},
		{"clicked", &types.StatsData{Data: &types.StatsData_Clicked{}}, stats.TypeClicked},
		{"bounced", &types.StatsData{Data: &types.StatsData_Bounced{}}, stats.TypeBounce},
		{"error", &types.StatsData{Data: &types.StatsData_Error{}}, stats.TypeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stats.DetermineType(tt.data)
			if got != tt.want {
				t.Errorf("DetermineType() = %s, want %s", got, tt.want)
			}
		})
	}
}

func assertBucket(t *testing.T, ag *stats.AggregatedStat, wantType stats.Type, wantTime time.Time, wantCount int64) {
	t.Helper()
	if ag.Type != wantType {
		t.Errorf("bucket type: got %s, want %s", ag.Type, wantType)
	}
	if !ag.Timestamp.Equal(wantTime) {
		t.Errorf("bucket time: got %v, want %v", ag.Timestamp, wantTime)
	}
	if ag.Count != wantCount {
		t.Errorf("bucket count: got %d, want %d", ag.Count, wantCount)
	}
}
