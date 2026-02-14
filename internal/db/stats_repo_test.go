package sqlc

import (
	"testing"

	"github.com/kannon-email/kannon/internal/stats"
)

func TestStatsRepository(t *testing.T) {
	repo := NewStatsRepository(q)
	stats.RunRepoSpec(t, repo)
}
