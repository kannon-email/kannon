package smtp

import (
	"context"
	"errors"
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

// LoadConfig reads the "smtp" section, defaults filled in.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := config.TryLoadSection(configKey, &cfg); err != nil {
		return Config{}, err
	}
	cfg.setDefaults()
	return cfg, nil
}

// Listen binds the inbound SMTP listener on cfg's address.
func Listen(cfg Config) (net.Listener, error) {
	return net.Listen("tcp", cfg.Address)
}

// New constructs the SMTP server runnable serving on l.
func New(cnt *container.Container, cfg Config, l net.Listener) container.Runnable {
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

	slog.Info("SMTP server listening", "addr", l.Addr().String())

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
	s.ErrorLog = newErrorLog()

	return s
}
