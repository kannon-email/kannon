package smtp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go"
)

// New constructs the SMTP server runnable, loading its slice of configuration
// from viper under the "smtp" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection("smtp", &cfg)
	cfg.setDefaults()
	return container.Runnable{
		Name: "smtp",
		Run: func(ctx context.Context) error {
			return run(ctx, cnt.Nats(), cfg)
		},
	}
}

func run(ctx context.Context, nc *nats.Conn, config Config) error {
	s := buildServer(config, nc)
	defer s.Close()

	slog.Info(fmt.Sprintf("Starting server at: %v", s.Addr))

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
	err := s.ListenAndServe()
	close(done)
	shutdownErr := <-stopped
	if err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

func buildServer(config Config, nc *nats.Conn) *smtp.Server {
	backend := &Backend{
		nc: nc,
	}

	s := smtp.NewServer(backend)

	s.Addr = config.Address
	s.Domain = config.Domain
	s.ReadTimeout = config.ReadTimeout
	s.WriteTimeout = config.WriteTimeout
	s.MaxMessageBytes = int64(config.MaxPayloadBytes)
	s.MaxRecipients = int(config.MaxRecipients)
	s.AllowInsecureAuth = true

	return s
}
