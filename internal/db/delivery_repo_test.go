package sqlc

import (
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
)

type deliveryTestHelper struct{}

func (deliveryTestHelper) CreateBatch(t *testing.T) (batch.ID, string) {
	return seedBatchFixture(t)
}

func TestDeliveryRepository(t *testing.T) {
	repo := NewDeliveryRepository(q)
	delivery.RunRepoSpec(t, repo, deliveryTestHelper{})
}
