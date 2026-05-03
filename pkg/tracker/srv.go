package tracker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/x/container"
	"github.com/sirupsen/logrus"
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
	logrus.Infof("running tracker on %s", addr)

	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("error shutting down server: %v", err)
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
