package smtp

import (
	"context"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

// New constructs the SMTP server runnable, loading its slice of configuration
// from viper under the "smtp" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	container.LoadConfig("smtp", &cfg)
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

	logrus.Printf("Starting server at: %v", s.Addr)

	go func() {
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			logrus.Errorf("error shutting down server: %v", err)
		}
	}()

	return s.ListenAndServe()
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
