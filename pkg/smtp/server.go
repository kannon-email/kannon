package smtp

import (
	"context"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

func Run(ctx context.Context, cnt *container.Container, config Config) error {
	nc := cnt.Nats()

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

	s.Addr = config.GetAddress()
	s.Domain = config.GetDomain()
	s.ReadTimeout = config.GetReadTimeout()
	s.WriteTimeout = config.GetWriteTimeout()
	s.MaxMessageBytes = int64(config.GetMaxPayload())
	s.MaxRecipients = int(config.GetMaxRecipients())
	s.AllowInsecureAuth = true

	return s
}
