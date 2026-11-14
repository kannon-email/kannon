package dispatcher

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/kannon-email/kannon/internal/statssec"
)

func TestADispatchThatCannotBuildIsCountedAsABuildErrorAndItsSpentDeliveryAsFailed(t *testing.T) {
	batchID, domain := seedReclaimBatch(t)
	email := "unbuildable@" + domain

	repo := sqlc.NewDeliveryRepository(testDB, delivery.DefaultBackoff, time.Nanosecond)
	claimer := pool.NewClaimer(repo)
	q := sqlc.New(testDB)
	reg := prometheus.NewRegistry()
	d := &disp{
		claimer: claimer,
		eb:      envelope.NewBuilder(q, statssec.NewStatsService(q), returnpath.MustParse(strings.Repeat("s", returnpath.MinSecretLength)), 0),
		pub:     &subjectPublisher{},
		flow:    mailflow.New(reg),
	}
	scheduleValidated(t, repo, claimer, batchID, domain, email)

	require.NoError(t, d.DispatchCycle(t.Context()))

	want := `
# HELP kannon_dispatch_total Dispatches of a claimed Delivery by the Dispatcher, by how they ended.
# TYPE kannon_dispatch_total counter
kannon_dispatch_total{result="build_error"} 1
# HELP kannon_delivery_outcomes_total Delivery outcomes, counted by the Component that decides them. source and permanent are set only for bounced.
# TYPE kannon_delivery_outcomes_total counter
kannon_delivery_outcomes_total{outcome="failed",permanent="",source=""} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_dispatch_total", "kannon_delivery_outcomes_total"))
}
