package cmd

import (
	"context"
	"fmt"

	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/bump"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/sender"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

var (
	cfgFile string

	rootCmd = &cobra.Command{
		Use:   "kannon",
		Short: "A massive send mail tool for kubernetes",
		Long:  `Kannon is an open source tool for sending massive emails on a kubernetes environment`,
		Run:   run,
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	config, err := readConfig()
	if err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}

	cnt := container.New(ctx, container.Config{
		DBUrl:   config.DatabaseURL,
		NatsURL: config.NatsURL,
		SenderConfig: container.SenderConfig{
			DemoSender: config.Sender.DemoSender,
			Hostname:   config.Sender.Hostname,
		},
	})
	defer cnt.Close()

	g, ctx := errgroup.WithContext(ctx)

	if config.RunSender {
		g.Go(func() error {
			err := runSender(ctx, cnt, config)
			if err != nil {
				return fmt.Errorf("error in sender: %v", err)
			}
			return nil
		})
	}

	if config.RunDispatcher {
		g.Go(func() error {
			err := runDispatcher(ctx, cnt, config)
			if err != nil {
				return fmt.Errorf("error in dispatcher: %v", err)
			}
			return nil
		})
	}

	if config.RunVerifier {
		g.Go(func() error {
			err := validator.Run(ctx, cnt)
			if err != nil {
				return fmt.Errorf("error in verifier: %v", err)
			}
			return nil
		})
	}

	if config.RunStats {
		g.Go(func() error {
			err := stats.Run(ctx, cnt)
			if err != nil {
				return fmt.Errorf("error in stats: %v", err)
			}
			return nil
		})
	}

	if config.RunBounce {
		g.Go(func() error {
			err := bump.Run(ctx, cnt)
			if err != nil {
				return fmt.Errorf("error in bounce: %v", err)
			}
			return nil
		})
	}

	if config.RunAPI {
		g.Go(func() error {
			err := runAPI(ctx, cnt, config)
			if err != nil {
				return fmt.Errorf("error in api: %v", err)
			}
			return nil
		})
	}

	if config.RunSMTP {
		g.Go(func() error {
			err := runSMTP(ctx, cnt, config)
			if err != nil {
				return fmt.Errorf("error in smtp: %v", err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		logrus.Fatalf("service error: %v", err)
	}
}

func init() {
	cobra.OnInitialize(prepareConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
	rootCmd.PersistentFlags().Bool("viper", true, "use Viper for configuration")
	createBoolFlagAndBindToViper("run-sender", false, "run sender")
	createBoolFlagAndBindToViper("run-dispatcher", false, "run dispatcher")
	createBoolFlagAndBindToViper("run-verifier", false, "run verifier")
	createBoolFlagAndBindToViper("run-bounce", false, "run bounce")
	createBoolFlagAndBindToViper("run-stats", false, "run stats")
	createBoolFlagAndBindToViper("run-api", false, "run api")
	createBoolFlagAndBindToViper("run-smtp", false, "run smtp server")
}

//nolint:unparam
func createBoolFlagAndBindToViper(name string, defaultValue bool, usage string) {
	rootCmd.PersistentFlags().Bool(name, defaultValue, usage)
	err := viper.BindPFlag(name, rootCmd.PersistentFlags().Lookup(name))
	if err != nil {
		logrus.Fatalf("cannot set flat '%v': %v", name, err)
	}
}

func runAPI(ctx context.Context, cnt *container.Container, config Config) error {
	cnf := config.API.ToAPIConfig()
	return api.Run(ctx, cnf, cnt)
}

func runDispatcher(ctx context.Context, cnt *container.Container, _ Config) error {
	return dispatcher.Run(ctx, cnt)
}

func runSender(ctx context.Context, cnt *container.Container, config Config) error {
	cnf := config.Sender.ToSenderConfig()
	sender := sender.NewSenderFromContainer(cnt, cnf)
	return sender.Run(ctx)
}

func runSMTP(ctx context.Context, cnt *container.Container, config Config) error {
	cnf := config.SMTP.ToSMTPConfig()
	conn := cnt.Nats()
	return smtp.Run(ctx, conn, cnf)
}
