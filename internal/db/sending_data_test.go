package sqlc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kannon-email/kannon/internal/batch"
)

// A Batch with no row, as when retention removed it under a Delivery still being built, is named
// with the domain error rather than the driver's, so callers above the seam need not know pgx.
func TestSendingDataOfAMissingBatch(t *testing.T) {
	_, err := NewSendingDataSource(db).GetSendingData(t.Context(), batch.ID("msg_missing@nowhere.test"))
	assert.ErrorIs(t, err, batch.ErrBatchNotFound)
}
