package sqlc

import (
	"testing"

	"github.com/kannon-email/kannon/internal/stats"
)

func TestAggregatedStatsRepository(t *testing.T) {
	repo := NewAggregatedStatsRepository(db)
	stats.RunAggregatedRepoSpec(t, repo)
}
