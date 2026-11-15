package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/x/container"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Listener is the Runnable serving GET /metrics from g on cfg.Port, and false when cfg disables it.
func Listener(cfg Config, g prometheus.Gatherer) (container.Runnable, bool) {
	if !cfg.Enabled {
		return container.Runnable{}, false
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(g, promhttp.HandlerOpts{}))
	server := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return container.Runnable{
		Name: "metrics",
		Run: func(ctx context.Context) error {
			slog.Info("metrics listening", "addr", server.Addr)
			return container.ServeHTTP(ctx, server)
		},
	}, true
}
