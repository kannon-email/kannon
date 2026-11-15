package e2e_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	dbschema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/x/startup"
)

// TestInfrastructure is where the scenarios reach the Kannon instance under test.
type TestInfrastructure struct {
	dbURL       string
	natsURL     string
	apiPort     uint
	restPort    uint
	healthPort  uint
	trackerPort uint
	smtpPort    uint
	metricsPort uint
}

// smtpAddr is where the bounce-receiving SMTP server listens: the address a
// remote MTA would deliver a DSN to.
func (infra *TestInfrastructure) smtpAddr() string {
	return fmt.Sprintf("localhost:%d", infra.smtpPort)
}

const configTemplate = `
database_url: %q
nats_url: %q
services:
  api: {enabled: true}
  dispatcher: {enabled: true}
  validator: {enabled: true}
  stats: {enabled: true}
  audit: {enabled: true}
  tracker: {enabled: true}
  smtp: {enabled: true}
  sender: {enabled: true}
api:
  port: 0
  rest_port: 0
  health_port: 0
  admin_token: %q
# Shared by the Dispatcher, which signs every return path with it, and the inbound SMTP server,
# which accepts a DSN only on a return path that verifies — so the bounce tests prove the two agree.
bounce:
  secret: e2e-bounce-secret-0123456789abcdef
tracker:
  port: 0
smtp:
  address: 127.0.0.1:0
stats:
  retention: 8760h
# Off by default (ADR 0010); on here because it is the only seam that proves the whole chain from a
# real call to an audit row.
audit:
  enabled: true
  retention: 720h
metrics:
  port: 0
`

// startKannon prepares Postgres with the real migrations and an in-process NATS, writes a config
// file the way an operator would, and boots Kannon through the production startup module.
func startKannon(t *testing.T, sender *senderMock) *TestInfrastructure {
	t.Helper()

	dbURL, purge, err := tests.TestPostgresURL()
	if err != nil {
		t.Fatalf("cannot start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := purge(); err != nil {
			t.Logf("cannot remove postgres: %v", err)
		}
	})
	if err := dbschema.Migrate(dbURL); err != nil {
		t.Fatalf("cannot migrate the database: %v", err)
	}
	natsURL := tests.NatsURL(t)

	path := filepath.Join(t.TempDir(), "kannon.yaml")
	if err := os.WriteFile(path, fmt.Appendf(nil, configTemplate, dbURL, natsURL, adminToken), 0o600); err != nil {
		t.Fatal(err)
	}

	inst, err := startup.Start(t.Context(), path,
		startup.WithSender(sender),
		// Collapse the production 2m/5m retry curve into milliseconds so the
		// transient-then-deliver path converges in CI wall time.
		startup.WithBackoff(delivery.ExponentialBackoff{
			Base: 50 * time.Millisecond,
			Min:  50 * time.Millisecond,
		}),
		// The Retry Budget has to be scaled by the same factor as the backoff base, or a collapsed
		// curve races through the whole budget. Production is 24h against 2m·2ⁿ; this curve is
		// 50ms·2ⁿ, 2400× faster, and 24h/2400 = 36s — which keeps the ten-retry boundary exactly.
		startup.WithRetryWindow(36*time.Second),
		// Sized for the parallel subtest burst: ~120+ concurrent messages, and a small pool blows
		// the EventuallyWithT windows.
		startup.WithSenderConcurrency(50),
	)
	if err != nil {
		t.Fatalf("cannot start kannon: %v", err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("error closing kannon: %v", err)
		}
	})

	failed := make(chan error, 1)
	go func() { failed <- inst.Wait() }()
	select {
	case <-inst.Ready():
	case err := <-failed:
		t.Fatalf("kannon stopped before it was ready: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("kannon was not ready within 60 seconds")
	}

	addrs := inst.Addrs()
	return &TestInfrastructure{
		dbURL:       dbURL,
		natsURL:     natsURL,
		apiPort:     port(addrs.API),
		restPort:    port(addrs.REST),
		healthPort:  port(addrs.Health),
		trackerPort: port(addrs.Tracker),
		smtpPort:    port(addrs.SMTP),
		metricsPort: port(addrs.Metrics),
	}
}

func port(addr net.Addr) uint {
	return uint(addr.(*net.TCPAddr).Port) //nolint:errcheck,forcetypeassert // every Kannon listener is TCP
}

// scrapedMetrics reads the metrics listener's /metrics endpoint as a prometheus.Gatherer.
type scrapedMetrics struct {
	infra *TestInfrastructure
}

func (s scrapedMetrics) Gather() ([]*dto.MetricFamily, error) {
	res, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", s.infra.metricsPort))
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape answered %d", res.StatusCode)
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(res.Body)
	if err != nil {
		return nil, err
	}
	out := make([]*dto.MetricFamily, 0, len(families))
	for _, f := range families {
		out = append(out, f)
	}
	return out, nil
}
