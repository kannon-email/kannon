package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/x/container"
)

type srv struct {
	pub publisher.Publisher
	ss  statssec.StatsService
	cfg Config
}

func NewServer(cnt *container.Container, cfg Config) *srv {
	q := cnt.Queries()
	ss := statssec.NewStatsService(q)

	return &srv{
		pub: cnt.Nats(),
		ss:  ss,
		cfg: cfg,
	}
}

func (s *srv) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/o/", s.handleOpen)
	mux.HandleFunc("/c/", s.handleClick)

	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.Port)
	slog.Info(fmt.Sprintf("running tracker on %s", addr))

	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("error shutting down server", "err", err)
		}
	}()

	return server.ListenAndServe()
}

func readUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}
