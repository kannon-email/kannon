package metrics

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/x/container"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Listen binds the metrics listener on cfg.Port. The caller decides whether metrics are enabled.
func Listen(cfg Config) (net.Listener, error) {
	return container.Listen(cfg.Port)
}

// Listener is the Runnable serving GET /metrics from g on l.
func Listener(l net.Listener, g prometheus.Gatherer) container.Runnable {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(g, promhttp.HandlerOpts{}))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return container.Runnable{
		Name: "metrics",
		Run: func(ctx context.Context, ready func()) error {
			slog.Info("Metrics listening on " + l.Addr().String())
			ready()
			return container.ServeHTTP(ctx, server, l)
		},
	}
}
