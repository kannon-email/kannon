package validator_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/pkg/validator"
)

func TestValidatorCountsTheOutcomeOfEveryDeliveryItDecides(t *testing.T) {
	reg := prometheus.NewRegistry()
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow))
	v := validator.NewValidator(claimer, &MockPublisher{}, mailflow.New(reg))

	domain := createTestDomain(t)
	sendEmail(t, domain, "valid@email.com")
	sendEmail(t, domain, "invalid-email.com")

	require.NoError(t, runner.Run(t.Context(), v.Cycle, runner.MaxLoop(1)))

	want := `
# HELP kannon_delivery_outcomes_total Delivery outcomes, counted by the Component that decides them. source and permanent are set only for bounced.
# TYPE kannon_delivery_outcomes_total counter
kannon_delivery_outcomes_total{outcome="rejected",permanent="",source=""} 1
kannon_delivery_outcomes_total{outcome="validated",permanent="",source=""} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_delivery_outcomes_total"))
}
