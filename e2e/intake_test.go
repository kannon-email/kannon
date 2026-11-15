package e2e_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/tests"
)

var intakeScenarios = map[string]func(*testing.T, *env){
	"BatchAboveDomainTrackingCeiling": testBatchAboveDomainTrackingCeiling,
	"EveryRecipientRejected":          testEveryRecipientRejected,
}

// testBatchAboveDomainTrackingCeiling: a Batch above its Domain's ceiling fails outright rather than
// being silently clamped (ADR 0003). The same Batch without the override is accepted, so the
// refusal is the ceiling's and nothing else's.
func testBatchAboveDomainTrackingCeiling(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Off, restclient.Off)
	rcpt := tests.FakeEmail(t)

	batch := tn.Batch("Batch Above Domain Ceiling Test", "<h1>Hello!</h1>", To(rcpt, nil))
	full := restclient.Full
	batch.Tracking = &restclient.TrackingOverride{Opens: &full}
	res := tn.TrySend(t, batch)

	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode(), string(res.Body))
	require.NotNil(t, res.ApplicationproblemJSON422)
	assert.Equal(t, "validation_failed", res.ApplicationproblemJSON422.Code)

	batch.Tracking = nil
	tn.Send(t, batch)
}

// testEveryRecipientRejected: a Batch in which nothing was accepted is not created, and the refusal
// accounts for every submitted Recipient.
func testEveryRecipientRejected(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Identified, restclient.Identified)
	greedy := tests.FakeEmail(t)

	full := "full"
	greedyIn := To(greedy, nil)
	greedyIn.Tracking = &restclient.RecipientTracking{Opens: &full, Links: &full}
	res := tn.TrySend(t, tn.Batch("Every Recipient Rejected Test", "<h1>Hello!</h1>", To("", nil), greedyIn))

	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode(), string(res.Body))
	problem := res.ApplicationproblemJSON422
	require.NotNil(t, problem)
	assert.Equal(t, "no_recipients_accepted", problem.Code)
	require.NotNil(t, problem.Errors)
	codes := map[string]string{}
	for _, f := range *problem.Errors {
		codes[f.Pointer] = f.Code
	}
	assert.Equal(t, map[string]string{
		"/recipients/0/email":    "invalid_email",
		"/recipients/1/tracking": "tracking_above_ceiling",
	}, codes)
	assert.Zero(t, e.MX.Attempts(greedy), "a Rejected Recipient is never sent to")
}
