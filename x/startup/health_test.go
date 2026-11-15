package startup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/x/container"
)

func TestHealthIsUnavailableUntilEveryComponentIsReady(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	dbURL, purge, err := tests.TestPostgresURL()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = purge() })

	path := filepath.Join(t.TempDir(), "kannon.yaml")
	config := fmt.Sprintf(`
database_url: %q
services:
  api: {enabled: true}
api:
  port: 0
  rest_port: 0
  health_port: 0
  admin_token: startup-test-admin-token-0123456789abcdef
metrics:
  enabled: false
`, dbURL)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	slow := container.Runnable{Name: "slow", Run: func(ctx context.Context, ready func()) error {
		select {
		case <-release:
			ready()
		case <-ctx.Done():
			return nil
		}
		<-ctx.Done()
		return nil
	}}

	inst, err := Start(t.Context(), path, withRunnable(slow))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })
	health := "http://" + inst.Addrs().Health.String() + "/health"

	deadline := time.Now().Add(10 * time.Second)
	for {
		status, err := statusOf(health)
		if err == nil {
			if status != http.StatusServiceUnavailable {
				t.Fatalf("/health answered %d before every component was ready, want 503", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/health never answered: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := inst.Ready(ctx); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if status, err := statusOf(health); err != nil || status != http.StatusOK {
		t.Fatalf("/health answered %d (%v) once ready, want 200", status, err)
	}
}

func statusOf(url string) (int, error) {
	res, err := http.Get(url) //nolint:gosec,noctx // a local test endpoint
	if err != nil {
		return 0, err
	}
	_ = res.Body.Close()
	return res.StatusCode, nil
}
