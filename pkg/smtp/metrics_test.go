package smtp

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnAsyncDSNIsCountedAsAnAsyncBounceByReplyClass(t *testing.T) {
	reg := prometheus.NewRegistry()
	flow := mailflow.New(reg)

	for _, diagnostic := range []string{"Diagnostic-Code: SMTP; 550 No such recipient", "Diagnostic-Code: SMTP; 450 Mailbox unavailable"} {
		s := &Session{To: bounceReturnPath, nc: &capturingPublisher{}, rp: testReturnPath, flow: flow}
		require.NoError(t, s.Data(strings.NewReader(dsn(diagnostic))))
	}
	s := &Session{To: "someone@example.com", nc: &capturingPublisher{}, rp: testReturnPath, flow: flow}
	require.NoError(t, s.Data(strings.NewReader(dsn("Diagnostic-Code: SMTP; 550 No such recipient"))))

	want := `
# HELP kannon_delivery_outcomes_total Delivery outcomes, counted by the Component that decides them. source and permanent are set only for bounced.
# TYPE kannon_delivery_outcomes_total counter
kannon_delivery_outcomes_total{outcome="bounced",permanent="false",source="async"} 1
kannon_delivery_outcomes_total{outcome="bounced",permanent="true",source="async"} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_delivery_outcomes_total"))
}
