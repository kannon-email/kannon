package sqlc

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolCensusCountsThePoolByStatus(t *testing.T) {
	ctx := t.Context()
	census := NewPoolCensus(db)
	before, err := census.DeliveriesByStatus(ctx)
	require.NoError(t, err)

	batchID, domain := seedBatchFixture(t)
	repo := NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	var ds []*delivery.Delivery
	for _, email := range []string{"a@" + domain, "b@" + domain, "c@" + domain} {
		d, err := delivery.New(delivery.NewParams{BatchID: batchID, Email: email, Domain: domain, ScheduledTime: time.Now()})
		require.NoError(t, err)
		ds = append(ds, d)
	}
	require.NoError(t, repo.Schedule(ctx, ds...))
	require.NoError(t, pool.NewClaimer(repo).MarkValidated(ctx, ds[0]))

	after, err := census.DeliveriesByStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, after[pool.StatusToValidate]-before[pool.StatusToValidate])
	assert.Equal(t, 1, after[pool.StatusScheduled]-before[pool.StatusScheduled])
}

func TestEveryStoredPoolStatusMapsToAReportedOne(t *testing.T) {
	for _, s := range []SendingPoolStatus{SendingPoolStatusToValidate, SendingPoolStatusValidating, SendingPoolStatusScheduled, SendingPoolStatusSending} {
		reported, ok := poolStatuses[s]
		require.True(t, ok, s)
		assert.Contains(t, pool.Statuses(), reported)
	}
}
