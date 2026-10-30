package stats_test

import (
	"github.com/kannon-email/kannon/internal/stats"
	"testing"
	"time"
)

func TestAggregateSeriesFillsBuckets(t *testing.T) {
	s := stats.NewService(stats.NewInMemRepository(), stats.WithAggregatedStatsRepository(stats.NewInMemAggregatedStatsRepository()))
	start := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{start.Add(-time.Hour), start, start.Add(time.Hour), start.Add(6 * time.Hour)} {
		if err := s.IncrementAggregatedStat(t.Context(), exampleCom, at, stats.TypeOpened); err != nil {
			t.Fatal(err)
		}
	}
	buckets, err := s.QueryAggregateSeries(authorizedCtx(t), exampleCom, stats.TimeRange{Start: start, Stop: start.Add(6 * time.Hour)}, 3*time.Hour)
	if err != nil || len(buckets) != 2 {
		t.Fatal(buckets, err)
	}
	if buckets[0].Counts[stats.TypeOpened] != 2 || buckets[1].Counts[stats.TypeOpened] != 0 || !buckets[1].Start.Equal(start.Add(3*time.Hour)) {
		t.Fatal(buckets)
	}
}
