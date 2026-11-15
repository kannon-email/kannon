package smtp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go"
)

const configKey = "smtp"

// Listen binds the inbound SMTP listener on the address the "smtp" section sets.
func Listen() (net.Listener, error) {
	var cfg Config
	if err := config.TryLoadSection(configKey, &cfg); err != nil {
		return nil, err
	}
	cfg.setDefaults()
	return net.Listen("tcp", cfg.Address)
}

// New constructs the SMTP server runnable serving on l, loading its slice of
// configuration from viper under the "smtp" key.
func New(cnt *container.Container, l net.Listener) container.Runnable {
	var cfg Config
	config.LoadSection(configKey, &cfg)
	cfg.setDefaults()
	rp := returnpath.MustLoad()
	return container.Runnable{
		Name: "smtp",
		Run: func(ctx context.Context, ready func()) error {
			nc := cnt.Nats()
			ready()
			return run(ctx, l, nc, cfg, rp, mailflow.New(cnt.Metrics()))
		},
	}
}

func run(ctx context.Context, l net.Listener, nc *nats.Conn, config Config, rp returnpath.Signer, flow *mailflow.Metrics) error {
	s := buildServer(config, nc, rp, flow)
	defer s.Close()

	slog.Info(fmt.Sprintf("Starting server at: %v", l.Addr()))

	done := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := s.Shutdown(shutdownCtx)
			if err != nil {
				_ = s.Close()
			}
			stopped <- err
		case <-done:
			stopped <- nil
		}
	}()
	err := s.Serve(l)
	close(done)
	shutdownErr := <-stopped
	if err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

func buildServer(config Config, nc *nats.Conn, rp returnpath.Signer, flow *mailflow.Metrics) *smtp.Server {
	backend := &Backend{
		nc:   nc,
		rp:   rp,
		flow: flow,
	}

	s := smtp.NewServer(backend)

	s.Domain = config.Domain
	s.ReadTimeout = config.ReadTimeout
	s.WriteTimeout = config.WriteTimeout
	s.MaxMessageBytes = int64(config.MaxPayloadBytes)
	s.MaxRecipients = int(config.MaxRecipients)
	s.AllowInsecureAuth = true

	return s
}
