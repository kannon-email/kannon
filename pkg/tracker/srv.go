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

// engagement is what an engagement event may keep: who it came from, and what the
// request that produced it revealed. A field left empty is a field the Tracking
// Mode did not permit, and the event goes out that way.
type engagement struct {
	email     string
	ip        string
	userAgent string
}

// retained returns what may be kept about an engagement, given the Tracking Mode
// signed into its token.
//
// A Mode that cannot isolate one Recipient of a Batch from another keeps nothing
// at all: Off and Anonymous are counted in aggregate only, so the event names
// nobody. The token such an engagement arrives on already names nobody — under
// Anonymous internal/statssec mints the sentinel address, and an older build left
// the claim empty — and dropping the claim here as well means no token, however
// it was minted, can put an identity on an event that must carry none.
//
// Pseudonymous is the first rung that does isolate, and it keeps the identity its
// token claims for exactly that reason: the claim is a pseudonym drawn per
// Delivery (ADR 0006), and an event that dropped it would be linkable to nothing,
// which is the whole content of the rung. It still names no Recipient, so it sits
// below Identified on the scale even though both keep their claim.
//
// Above that, only Full retains the IP address and user agent (CONTEXT.md), so
// under Pseudonymous and Identified the event carries its identity and nothing
// more.
//
// The Mode is read from the verified claims and never from the database, so this
// is the single place the decision is made and the request cannot widen it.
func retained(r *http.Request, claimedEmail string, mode tracking.Mode) engagement {
	if !mode.IsolatesRecipient() {
		return engagement{}
	}
	if mode != tracking.ModeFull {
		return engagement{email: claimedEmail}
	}
	return engagement{email: claimedEmail, ip: readUserIP(r), userAgent: r.UserAgent()}
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
