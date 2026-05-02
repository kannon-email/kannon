package sqlc

import (
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/stretchr/testify/require"
)

type claimerTestHelper struct {
	deliveries delivery.Repository
}

func (h claimerTestHelper) CreateBatch(t *testing.T) (batch.ID, string) {
	return seedBatchFixture(t)
}

func (h claimerTestHelper) Schedule(t *testing.T, d *delivery.Delivery) {
	t.Helper()
	require.NoError(t, h.deliveries.Schedule(t.Context(), d))
}

func TestPoolClaimer(t *testing.T) {
	deliveries := NewDeliveryRepository(q)
	c := pool.NewClaimer(deliveries)
	pool.RunClaimerSpec(t, c, claimerTestHelper{deliveries: deliveries})
}
