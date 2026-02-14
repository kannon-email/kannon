package stats

import "testing"

func TestInMemAggregatedStatsRepository(t *testing.T) {
	repo := NewInMemAggregatedStatsRepository()
	RunAggregatedRepoSpec(t, repo)
}
