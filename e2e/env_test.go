package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	netsmtp "net/smtp"
	"os"
	"path/filepath"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	dbschema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/x/startup"
)

// adminToken is the operator credential Kannon is configured with. Only the env's admin client
// carries it: a Tenant sends and reads with its own Domain's API Key, so the two are never confused.
const adminToken = "e2e-admin-token-0123456789abcdef0123"

// env is the Kannon instance under test, seen from outside: its REST API, the edges a scenario may
// play (the remote MX, a remote MTA delivering a DSN, a recipient's client hitting the Tracker) and
// the metrics endpoint an operator scrapes.
type env struct {
	MX *capturingMX

	admin   *restclient.ClientWithResponses
	restURL string
	tracker string
	smtp    string
	metrics string
	// dbURL is read only by the audit scenarios, the documented exception until #515.
	dbURL string
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

// startEnv prepares Postgres with the real migrations and an in-process NATS, writes a config file
// the way an operator would, and boots Kannon through the production startup module.
func startEnv(t *testing.T) *env {
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

	mx := newCapturingMX()
	inst, err := startup.Start(t.Context(), path,
		startup.WithSender(mx),
		// Collapse the production 2m/5m retry curve into milliseconds so retry paths converge in CI
		// wall time.
		startup.WithBackoff(delivery.ExponentialBackoff{
			Base: 50 * time.Millisecond,
			Min:  50 * time.Millisecond,
		}),
		// Long enough that a transient Recipient retried behind a burst of parallel sends still gets
		// its retries, short enough that a Recipient whose MX always defers (50ms·2ⁿ) spends the
		// whole Retry Budget well inside one wait.
		startup.WithRetryWindow(15*time.Second),
		// Sized for the parallel subtest burst: ~120+ concurrent messages.
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

	readyCtx, cancel := context.WithTimeout(t.Context(), scaled(defaultTimeout))
	defer cancel()
	if err := inst.Ready(readyCtx); err != nil {
		t.Fatalf("kannon was not ready: %v", err)
	}

	addrs := inst.Addrs()
	e := &env{
		MX:      mx,
		restURL: "http://" + addrs.REST.String() + "/v1",
		tracker: "http://" + addrs.Tracker.String(),
		smtp:    addrs.SMTP.String(),
		metrics: "http://" + addrs.Metrics.String() + "/metrics",
		dbURL:   dbURL,
	}
	e.admin = e.client(t, adminToken)
	return e
}

func (e *env) client(t *testing.T, token string) *restclient.ClientWithResponses {
	t.Helper()
	c, err := restclient.NewClientWithResponses(e.restURL, restclient.WithRequestEditorFn(
		func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+token)
			return nil
		}))
	require.NoError(t, err)
	return c
}

// dsnTemplate is a delivery status notification in the multipart/report shape of RFC 3464, the
// format a remote MTA uses to report a delivery it accepted and could not complete.
const dsnTemplate = `From: MAILER-DAEMON@mx.example.com
To: %s
Subject: Undelivered Mail Returned to Sender
Content-Type: multipart/report; report-type=delivery-status; boundary="B"

--B
Content-Type: text/plain; charset=us-ascii

This is the mail system at host mx.example.com.

--B
Content-Type: message/delivery-status

Reporting-MTA: dns; mx.example.com
Final-Recipient: rfc822; %s
Action: failed
Diagnostic-Code: SMTP; %d %s

--B--
`

// DeliverDSN plays a remote MTA: it connects to Kannon's inbound SMTP server over a real socket and
// delivers a bounce report addressed to returnPath, which is the only thing tying the report back to
// a Batch and a Recipient. Kannon may refuse the transaction; that is reported, not failed, so a
// scenario can deliver a forged report and assert on what follows.
func (e *env) DeliverDSN(t *testing.T, returnPath, rcpt string, code int, reason string) error {
	t.Helper()

	c, err := netsmtp.Dial(e.smtp)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	if err := c.Mail("MAILER-DAEMON@mx.example.com"); err != nil {
		return err
	}
	if err := c.Rcpt(returnPath); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, dsnTemplate, returnPath, rcpt, code, reason); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// What a recipient's mail client carries into a tracking request. Under Full the Tracker retains
// exactly these two values; under any lower Mode, neither.
const (
	engagementIP        = "203.0.113.9"
	engagementUserAgent = "kannon-e2e-agent/1.0"
)

// Track issues one tracking request the way a recipient's client would: on the stats host of the
// Domain the link belongs to, from engagementIP with engagementUserAgent. Redirects are not
// followed; the status and the Location header are returned for the scenario to assert.
func (e *env) Track(t *testing.T, host, path string) (status int, location string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.tracker+path, nil)
	require.NoError(t, err)
	req.Host = host
	req.Header.Set("User-Agent", engagementUserAgent)
	req.Header.Set("X-Real-Ip", engagementIP)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body) //nolint:errcheck // drained only so the connection is reused
	return res.StatusCode, res.Header.Get("Location")
}

// Metrics scrapes the metrics listener's /metrics endpoint.
func (e *env) Metrics(c require.TestingT) scrape {
	res, err := http.Get(e.metrics) //nolint:noctx // a scrape is bounded by the wait that runs it
	requireRead(c, err)
	defer func() { _ = res.Body.Close() }()
	requireOK(c, res.StatusCode, nil)
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(res.Body)
	require.NoError(c, err)
	return families
}

// scrape is one read of /metrics, by metric family name.
type scrape map[string]*dto.MetricFamily

// Counter is the value of the first counter series carrying these labels, or zero if none was
// exported.
func (s scrape) Counter(name string, labels map[string]string) float64 {
	for _, m := range s[name].GetMetric() {
		if hasLabels(m, labels) {
			return m.GetCounter().GetValue()
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
