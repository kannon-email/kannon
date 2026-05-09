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

// seedBenchBatch is a benchmark variant of seedBatchFixture that does not
// register a t.Cleanup (benchmarks may run many iterations and the test
// framework also runs them under TestMain so cleanup is best-effort).
func seedBenchBatch(b *testing.B) (batch.ID, string) {
	b.Helper()
	ctx := context.Background()
	domainName := fmt.Sprintf("bench-pool-%d.com", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "bench-private",
		DkimPublicKey:  "bench-public",
	})
	require.NoError(b, err)

	tplID := fmt.Sprintf("bench-tpl-%d", time.Now().UnixNano())
	_, err = q.CreateTemplate(ctx, CreateTemplateParams{
		TemplateID: tplID,
		Html:       "<p>hi</p>",
		Domain:     domainName,
		Type:       TemplateTypeTransient,
	})
	require.NoError(b, err)

	bID := batch.NewID(domainName)
	_, err = q.CreateMessage(ctx, CreateMessageParams{
		MessageID:   bID.String(),
		Subject:     "hello",
		SenderEmail: "from@" + domainName,
		SenderAlias: "From",
		TemplateID:  tplID,
		Domain:      domainName,
		Attachments: Attachments{},
		Headers:     Headers{},
	})
	require.NoError(b, err)

	b.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.Exec(cleanupCtx, "DELETE FROM sending_pool_emails WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})

	return bID, domainName
}

func buildDeliveries(b *testing.B, batchID batch.ID, domain string, n, iter int) []*delivery.Delivery {
	b.Helper()
	now := time.Now().UTC()
	out := make([]*delivery.Delivery, n)
	for i := 0; i < n; i++ {
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
			batchID, domain := seedBenchBatch(b)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
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
