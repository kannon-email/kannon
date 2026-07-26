package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tracking"
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

	return newServer(cnt.Nats(), ss, cfg)
}

// newServer wires a tracker against explicit dependencies, so the handlers can
// be driven over HTTP without a container behind them.
func newServer(pub publisher.Publisher, ss statssec.StatsService, cfg Config) *srv {
	return &srv{
		pub: pub,
		ss:  ss,
		cfg: cfg,
	}
}

// handler is the tracker's routing table: the pixel endpoint and the tracked
// link endpoint.
func (s *srv) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/o/", s.handleOpen)
	mux.HandleFunc("/c/", s.handleClick)

	return mux
}

func (s *srv) Run(ctx context.Context) error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.Port)
	slog.Info("running tracker on " + addr)

	server := &http.Server{Addr: addr, Handler: s.handler()}

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

// retained returns what may be kept about the request behind an engagement
// event, given the Tracking Mode signed into its token. Only Full retains the IP
// address and user agent (CONTEXT.md); every other Mode yields neither, so under
// Identified the event carries the recipient identity and nothing more.
//
// A Mode is read from the verified claims and never from the database, so this is
// the single place the decision is made and it cannot be widened by the request.
func retained(r *http.Request, mode tracking.Mode) (ip string, userAgent string) {
	if mode != tracking.ModeFull {
		return "", ""
	}
	return readUserIP(r), r.UserAgent()
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
