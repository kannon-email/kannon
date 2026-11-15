package metrics_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
)

func TestListenerServesGoAndProcessMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cnt := container.New(ctx, config.RootConfig{})

	l, err := metrics.Listen(metrics.Config{Enabled: true, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	listener := metrics.Listener(l, cnt.Metrics())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx, func() {}) }()

	body := scrape(t, "http://"+l.Addr().String()+"/metrics")
	for _, want := range []string{"go_goroutines", "process_"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in the scrape", want)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listener stopped with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not stop with its context")
	}
}

func scrape(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("listener did not answer: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("scrape answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
