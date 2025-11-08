package cmd

import (
	"fmt"

	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/bump"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/sender"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var standaloneCmd = &cobra.Command{
	Use:   "standalone",
	Short: "Run Kannon in standalone mode with embedded NATS",
	Long: `Run all Kannon components (API, SMTP, Sender, Dispatcher, Verifier, Stats, Bounce)
in a single process with an embedded NATS server. This mode is ideal for development,
testing, or single-server deployments. You will still need a PostgreSQL database.`,
	Run: runStandalone,
}

func init() {
	rootCmd.AddCommand(standaloneCmd)
}

func runStandalone(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	config, err := readConfig()
	if err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}

	// Create container with embedded NATS enabled
	cnt := container.New(ctx, container.Config{
		DBUrl:           config.DatabaseURL,
		UseEmbeddedNats: true,
		SenderConfig: container.SenderConfig{
			DemoSender: config.Sender.DemoSender,
			Hostname:   config.Sender.Hostname,
		},
	})
	defer cnt.Close()

	// Initialize JetStream streams
	if err := initializeJetStream(cnt); err != nil {
		logrus.Fatalf("failed to initialize JetStream: %v", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	// Start all components
	logrus.Info("Starting all Kannon components...")

	// Sender
	g.Go(func() error {
		logrus.Info("Starting Sender component...")
		cnf := config.Sender.ToSenderConfig()
		sender := sender.NewSenderFromContainer(cnt, cnf)
		if err := sender.Run(ctx); err != nil {
			return fmt.Errorf("error in sender: %v", err)
		}
		return nil
	})

	// Dispatcher
	g.Go(func() error {
		logrus.Info("Starting Dispatcher component...")
		if err := dispatcher.Run(ctx, cnt); err != nil {
			return fmt.Errorf("error in dispatcher: %v", err)
		}
		return nil
	})

	// Verifier
	g.Go(func() error {
		logrus.Info("Starting Verifier component...")
		if err := validator.Run(ctx, cnt); err != nil {
			return fmt.Errorf("error in verifier: %v", err)
		}
		return nil
	})

	// Stats
	g.Go(func() error {
		logrus.Info("Starting Stats component...")
		if err := stats.Run(ctx, cnt); err != nil {
			return fmt.Errorf("error in stats: %v", err)
		}
		return nil
	})

	// Bounce
	g.Go(func() error {
		logrus.Info("Starting Bounce component...")
		if err := bump.Run(ctx, cnt); err != nil {
			return fmt.Errorf("error in bounce: %v", err)
		}
		return nil
	})

	// API
	g.Go(func() error {
		logrus.Info("Starting API component...")
		cnf := config.API.ToAPIConfig()
		if err := api.Run(ctx, cnf, cnt); err != nil {
			return fmt.Errorf("error in api: %v", err)
		}
		return nil
	})

	// SMTP
	g.Go(func() error {
		logrus.Info("Starting SMTP component...")
		cnf := config.SMTP.ToSMTPConfig()
		conn := cnt.Nats()
		if err := smtp.Run(ctx, conn, cnf); err != nil {
			return fmt.Errorf("error in smtp: %v", err)
		}
		return nil
	})

	logrus.Info("All components started successfully. Kannon is running in standalone mode.")

	if err := g.Wait(); err != nil {
		logrus.Fatalf("service error: %v", err)
	}
}

// initializeJetStreamStreams creates the required JetStream streams
// This function is idempotent - it checks if streams exist before creating them
func initializeJetStreamStreams(js nats.JetStreamContext) error {
	// Define the streams that Kannon uses
	streams := []struct {
		name     string
		subjects []string
	}{
		{
			name:     "kannon-sending",
			subjects: []string{"kannon.sending"},
		},
		{
			name:     "kannon-stats",
			subjects: []string{"kannon.stats.*"},
		},
		{
			name:     "kannon-bounce",
			subjects: []string{"kannon.bounce"},
		},
	}

	// Create streams if they don't exist
	for _, stream := range streams {
		_, err := js.StreamInfo(stream.name)
		if err != nil {
			// Stream doesn't exist, create it
			logrus.Infof("Creating JetStream stream: %s", stream.name)
			_, err = js.AddStream(&nats.StreamConfig{
				Name:     stream.name,
				Subjects: stream.subjects,
				Storage:  nats.FileStorage,
			})
			if err != nil {
				return fmt.Errorf("failed to create stream %s: %w", stream.name, err)
			}
		} else {
			logrus.Debugf("JetStream stream already exists: %s", stream.name)
		}
	}

	return nil
}

// initializeJetStream ensures JetStream streams are created
func initializeJetStream(cnt *container.Container) error {
	nc := cnt.Nats()
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return initializeJetStreamStreams(js)
}
