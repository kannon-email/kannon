package sqlc

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/intake"
)

// CleanupBatchHistory removes completed REST Batches once intake.HistoryRetention
// has passed. Receipts outlive them (see CleanupBatchReceipts); Stats and shared
// Templates are untouched.
func (q *Queries) CleanupBatchHistory(ctx context.Context, now time.Time) error {
	return q.CleanupExpiredBatchHistory(ctx, timestamptz(intake.HistoryCutoff(now)))
}

// CleanupMailerBatches removes Mailer API Batches, and their unshared Transient
// Templates, once the Pool is empty for them and intake.HistoryRetention has
// passed since intake. They have no receipt, so CleanupBatchHistory never
// reaches them.
func (q *Queries) CleanupMailerBatches(ctx context.Context, now time.Time) error {
	return q.CleanupExpiredMailerBatches(ctx, timestamptz(intake.HistoryCutoff(now)))
}

// CleanupBatchReceipts removes idempotency receipts once intake.ReceiptRetention
// has passed since their Batch completed and its history is gone. From then on
// the key no longer deduplicates.
func (q *Queries) CleanupBatchReceipts(ctx context.Context, now time.Time) error {
	return q.DeleteExpiredBatchReceipts(ctx, timestamptz(intake.ReceiptCutoff(now)))
}
