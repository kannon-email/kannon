package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/stretchr/testify/require"
)

func buildDeliveries(b *testing.B, batchID batch.ID, domain string, n, iter int) []*delivery.Delivery {
	b.Helper()
	now := time.Now().UTC()
	out := make([]*delivery.Delivery, n)
	for i := range n {
		d, err := delivery.New(delivery.NewParams{
			BatchID:       batchID,
			Email:         fmt.Sprintf("u%d-%d@%s", iter, i, domain),
			Fields:        map[string]string{"name": "X"},
			Domain:        domain,
			ScheduledTime: now,
			Backoff:       delivery.DefaultBackoff,
		})
		require.NoError(b, err)
		out[i] = d
	}
	return out
}

func BenchmarkScheduleMany(b *testing.B) {
	repo := NewDeliveryRepository(db, delivery.DefaultBackoff)
	ctx := context.Background()

	for _, n := range []int{10, 100, 1000, 10_000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			batchID, domain := seedBatchFixture(b)
			b.ResetTimer()
			for i := range b.N {
				b.StopTimer()
				ds := buildDeliveries(b, batchID, domain, n, i)
				b.StartTimer()
				if err := repo.Schedule(ctx, ds...); err != nil {
					b.Fatalf("schedule: %v", err)
				}
			}
		})
	}
}
