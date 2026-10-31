package sqlc

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/intake"
)

// CleanupBatchHistory removes completed REST Batches once intake.HistoryRetention
// has passed. Receipts remain bound independently; Stats and shared Templates
// are untouched.
func (q *Queries) CleanupBatchHistory(ctx context.Context, now time.Time) error {
	return q.CleanupExpiredBatchHistory(ctx, timestamptz(intake.HistoryCutoff(now)))
}
