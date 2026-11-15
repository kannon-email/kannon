package startup_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbschema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/x/startup"
)

func TestMain(m *testing.M) {
	tests.Main(m.Run)
}

const (
	adminToken   = "startup-test-admin-token-0123456789abcdef"
	bounceSecret = "startup-test-bounce-secret-0123456789"
)

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kannon.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStartRefusesAMisconfiguredProcessWithAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   string
	}{
		{"nothing enabled", "services: {}\n", "asked to run nothing"},
		{"API without an admin token", "services:\n  api:\n    enabled: true\n", "api.admin_token"},
		{"dispatcher without a bounce secret", "services:\n  dispatcher:\n    enabled: true\n", "bounce.secret"},
		{
			"metrics on the tracker's port",
			"services:\n  tracker:\n    enabled: true\ntracker:\n  port: 9191\nmetrics:\n  port: 9191\n",
			"metrics.port",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, err := startup.Start(t.Context(), writeConfig(t, tc.config))
			if err == nil {
				_ = inst.Close()
				t.Fatal("expected the boot to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected the refusal to name %q, got %q", tc.want, err)
			}
		})
	}
}

func TestStartBindsPortZeroAndBecomesReady(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	dbURL, purge, err := tests.TestPostgresURL()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := purge(); err != nil {
			t.Logf("cannot remove postgres: %v", err)
		}
	})
	if err := dbschema.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}

	path := writeConfig(t, fmt.Sprintf(`
database_url: %q
nats_url: %q
services:
  api: {enabled: true}
  tracker: {enabled: true}
  smtp: {enabled: true}
  dispatcher: {enabled: true}
  validator: {enabled: true}
  stats: {enabled: true}
api:
  port: 0
  rest_port: 0
  health_port: 0
  admin_token: %q
bounce:
  secret: %q
tracker:
  port: 0
smtp:
  address: 127.0.0.1:0
metrics:
  port: 0
`, dbURL, tests.NatsURL(t), adminToken, bounceSecret))

	inst, err := startup.Start(t.Context(), path)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })

	readyCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := inst.Ready(readyCtx); err != nil {
		t.Fatalf("the instance never became ready: %v", err)
	}

	addrs := inst.Addrs()
	for name, addr := range map[string]net.Addr{
		"api": addrs.API, "rest": addrs.REST, "health": addrs.Health,
		"tracker": addrs.Tracker, "smtp": addrs.SMTP, "metrics": addrs.Metrics,
	} {
		tcp, ok := addr.(*net.TCPAddr)
		if !ok || tcp.Port == 0 {
			t.Errorf("%s: expected a bound port, got %v", name, addr)
		}
	}

	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", addrs.Health.(*net.TCPAddr).Port)) //nolint:errcheck // checked above
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/health answered %d once ready, want 200", res.StatusCode)
	}

	if err := inst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
