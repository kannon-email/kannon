package mailflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThePoolGaugeReportsTheDeliveriesInEachStatusAtScrapeTime(t *testing.T) {
	reg := prometheus.NewRegistry()
	counts := map[string]float64{"to_validate": 3, "scheduled": 1}
	mailflow.RegisterPool(reg, func(context.Context) (map[string]float64, error) { return counts, nil })

	want := `
# HELP kannon_pool_deliveries Deliveries in the Pool, by status. The statuses are the Pool's internal mechanics, exposed for operators, and may change between releases.
# TYPE kannon_pool_deliveries gauge
kannon_pool_deliveries{status="scheduled"} 1
kannon_pool_deliveries{status="to_validate"} 3
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_pool_deliveries"))

	counts = map[string]float64{"sending": 2}
	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "a scrape reports the Pool as it is now")
}

func TestAPoolQueryThatFailsYieldsNoSampleAndHidesNoOtherMetric(t *testing.T) {
	reg := prometheus.NewRegistry()
	mailflow.New(reg).Dispatched(mailflow.DispatchOK)
	mailflow.RegisterPool(reg, func(context.Context) (map[string]float64, error) { return nil, errors.New("database unreachable") })

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries", "kannon_dispatch_total")
	require.NoError(t, err, "a failed Pool query must not fail the scrape")
	assert.Equal(t, 1, n)
}

func TestThePoolQueryIsBoundedByATimeout(t *testing.T) {
	reg := prometheus.NewRegistry()
	mailflow.RegisterPool(reg, func(ctx context.Context) (map[string]float64, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestRegisteringThePoolGaugeTwiceKeepsOne(t *testing.T) {
	reg := prometheus.NewRegistry()
	count := func(context.Context) (map[string]float64, error) { return map[string]float64{"sending": 1}, nil }
	mailflow.RegisterPool(reg, count)
	mailflow.RegisterPool(reg, count)

	n, err := testutil.GatherAndCount(reg, "kannon_pool_deliveries")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
