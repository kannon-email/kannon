package metrics_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
)

func TestListenerServesGoAndProcessMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cnt := container.NewForTest(ctx)
	port := freePort(t)

	listener, ok := metrics.Listener(metrics.Config{Enabled: true, Port: port}, cnt.Metrics())
	if !ok {
		t.Fatal("expected an enabled listener")
	}
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()

	body := scrape(t, fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
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

func TestDisabledMetricsOpenNoListener(t *testing.T) {
	cnt := container.NewForTest(t.Context())
	if _, ok := metrics.Listener(metrics.Config{Enabled: false, Port: 9090}, cnt.Metrics()); ok {
		t.Fatal("expected no listener when metrics are disabled")
	}
}

func freePort(t *testing.T) uint {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("not a TCP address: %v", l.Addr())
	}
	return uint(addr.Port)
}

func scrape(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := http.Get(url)
		if err == nil {
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
		if time.Now().After(deadline) {
			t.Fatalf("listener never answered: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
