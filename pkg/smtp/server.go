package smtp

import (
	"context"
	"fmt"

	"github.com/emersion/go-smtp"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func Run(ctx context.Context, config Config) error {
	natsURL := viper.GetString("nats_url")

	nc, _, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()

	s := buildServer(config, nc)
	defer s.Close()

	logrus.Printf("Starting server at: %v", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		return fmt.Errorf("error serving: %v", err)
	}

	return nil
}

func buildServer(config Config, nc *nats.Conn) *smtp.Server {
	be := &Backend{
		nc: nc,
	}

	s := smtp.NewServer(be)
	defer s.Close()

	s.Addr = config.GetAddress()
	s.Domain = config.GetDomain()
	s.ReadTimeout = config.GetReadTimeout()
	s.WriteTimeout = config.GetWriteTimeout()
	s.MaxMessageBytes = int64(config.GetMaxPayload())
	s.MaxRecipients = int(config.GetMaxRecipients())
	s.AllowInsecureAuth = true

	return s
}
