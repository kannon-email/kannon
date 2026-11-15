package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/internal/tests"
)

// testMetrics sends one Batch with one accepted and one rejected Recipient and reads the mail-flow
// counters through a scrape of the metrics listener, the surface an operator's Prometheus reads.
func testMetrics(t *testing.T, e *env) {
	tn := e.NewTenant(t)

	watched := []struct {
		name   string
		labels map[string]string
	}{
		{"kannon_batches_total", map[string]string{"intake": "rest"}},
		{"kannon_recipients_total", map[string]string{"intake": "rest", "result": "accepted"}},
		{"kannon_recipients_total", map[string]string{"intake": "rest", "result": "rejected"}},
		{"kannon_delivery_outcomes_total", map[string]string{"outcome": "validated"}},
		{"kannon_delivery_outcomes_total", map[string]string{"outcome": "delivered"}},
		{"kannon_send_attempts_total", map[string]string{"result": "delivered"}},
		{"kannon_dispatch_total", map[string]string{"result": "ok"}},
	}
	var before scrape
	eventually(t, "a first scrape", func(c *probe) { before = e.Metrics(c) })

	valid := tests.FakeEmail(t)
	b := tn.Send(t, tn.Batch("Mail Flow Metrics", "<h1>Hello!</h1>", To(valid, nil), To("", nil)))
	require.Equal(t, 1, b.AcceptedCount)
	tn.AwaitBatch(t, b, Completed)

	eventually(t, "the mail-flow counters moved by one", func(c *probe) {
		after := e.Metrics(c)
		for _, s := range watched {
			assert.Equal(c, 1.0, after.Counter(s.name, s.labels)-before.Counter(s.name, s.labels), "%s %v", s.name, s.labels)
		}
	})
}
