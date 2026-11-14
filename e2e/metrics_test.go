package e2e_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/tests"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

type series struct {
	name   string
	labels map[string]string
}

func testMailFlowMetrics(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure, reg prometheus.Gatherer) {
	client := clientFactory.NewClient(t, infra)

	watched := []series{
		{"kannon_batches_total", map[string]string{"intake": "mailer"}},
		{"kannon_recipients_total", map[string]string{"intake": "mailer", "result": "accepted"}},
		{"kannon_recipients_total", map[string]string{"intake": "mailer", "result": "rejected"}},
		{"kannon_delivery_outcomes_total", map[string]string{"outcome": "validated"}},
		{"kannon_delivery_outcomes_total", map[string]string{"outcome": "delivered"}},
		{"kannon_send_attempts_total", map[string]string{"result": "delivered"}},
		{"kannon_dispatch_total", map[string]string{"result": "ok"}},
	}
	before := map[int]float64{}
	for i, s := range watched {
		before[i] = counterValue(t, reg, s)
	}

	valid := tests.FakeEmail(t)
	res := client.SendEmailResponse(t, &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: valid}, {Email: ""}},
		Subject:       "Mail Flow Metrics",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	})
	require.EqualValues(t, 1, res.AcceptedCount)
	requireGetEmail(t, senderMock, valid)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		for i, s := range watched {
			assert.Equal(c, 1.0, counterValue(t, reg, s)-before[i], "%s %v", s.name, s.labels)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

func counterValue(t *testing.T, reg prometheus.Gatherer, s series) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != s.name {
			continue
		}
		for _, m := range f.GetMetric() {
			if hasLabels(m, s.labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func hasLabels(m *dto.Metric, want map[string]string) bool {
	matched := 0
	for _, l := range m.GetLabel() {
		v, ok := want[l.GetName()]
		if !ok {
			continue
		}
		if v != l.GetValue() {
			return false
		}
		matched++
	}
	return matched == len(want)
}
