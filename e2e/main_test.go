package e2e_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tests"
)

func TestMain(m *testing.M) {
	tests.Main(m.Run)
}

// TestE2E boots one Kannon instance and runs every scenario against it from the outside, each in a
// Tenant of its own. Scenarios are grouped by feature; the groups run in parallel with each other.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	e := startEnv(t)

	// Not parallel, and first: it reads process-wide counters, which move for its Batch alone only
	// while nothing else is sending.
	t.Run("Metrics", func(t *testing.T) { testMetrics(t, e) })

	features := []struct {
		name      string
		scenarios map[string]func(*testing.T, *env)
	}{
		{"Sending", sendingScenarios},
		{"Bounces", bounceScenarios},
		{"Tracking", trackingScenarios},
		{"Intake", intakeScenarios},
		{"Audit", auditScenarios},
	}
	for _, f := range features {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			for name, scenario := range f.scenarios {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					scenario(t, e)
				})
			}
		})
	}
}
