package mailflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type census func(context.Context) (map[pool.Status]int, error)

func (c census) DeliveriesByStatus(ctx context.Context) (map[pool.Status]int, error) {
	return c(ctx)
}

const poolHeader = `
# HELP kannon_pool_deliveries Deliveries in the Pool, by status. The statuses are the Pool's internal mechanics, exposed for operators, and may change between releases.
# TYPE kannon_pool_deliveries gauge
`

func TestThePoolGaugeReportsEveryStatusAtScrapeTimeZeroWhenEmpty(t *testing.T) {
	reg := prometheus.NewRegistry()
	counts := map[pool.Status]int{pool.StatusToValidate: 3, pool.StatusScheduled: 1}
	mailflow.RegisterPool(reg, census(func(context.Context) (map[pool.Status]int, error) { return counts, nil }))

	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(poolHeader+`
kannon_pool_deliveries{status="scheduled"} 1
kannon_pool_deliveries{status="sending"} 0
kannon_pool_deliveries{status="to_validate"} 3
kannon_pool_deliveries{status="validating"} 0
`), "kannon_pool_deliveries"))

	counts = map[pool.Status]int{pool.StatusSending: 2}
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(poolHeader+`
kannon_pool_deliveries{status="scheduled"} 0
kannon_pool_deliveries{status="sending"} 2
kannon_pool_deliveries{status="to_validate"} 0
kannon_pool_deliveries{status="validating"} 0
`), "kannon_pool_deliveries"), "a scrape reports the Pool as it is now, and no status series vanishes")
}

func TestThePoolGaugeNeverReportsAStatusItDoesNotKnow(t *testing.T) {
	reg := prometheus.NewRegistry()
	mailflow.RegisterPool(reg, census(func(context.Context) (map[pool.Status]int, error) {
		return map[pool.Status]int{"sent": 5}, nil
	}))

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, len(pool.Statuses()), n)
}

func TestAPoolQueryThatFailsYieldsNoSampleAndHidesNoOtherMetric(t *testing.T) {
	reg := prometheus.NewRegistry()
	mailflow.New(reg).Dispatched(mailflow.DispatchOK)
	mailflow.RegisterPool(reg, census(func(context.Context) (map[pool.Status]int, error) {
		return nil, errors.New("database unreachable")
	}))

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries", "kannon_dispatch_total")
	require.NoError(t, err, "a failed Pool query must not fail the scrape")
	assert.Equal(t, 1, n)
}

func TestThePoolQueryIsBoundedByATimeout(t *testing.T) {
	reg := prometheus.NewRegistry()
	mailflow.RegisterPool(reg, census(func(ctx context.Context) (map[pool.Status]int, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestRegisteringThePoolGaugeTwiceKeepsOne(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := census(func(context.Context) (map[pool.Status]int, error) {
		return map[pool.Status]int{pool.StatusSending: 1}, nil
	})
	mailflow.RegisterPool(reg, c)
	mailflow.RegisterPool(reg, c)

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, len(pool.Statuses()), n)
}
