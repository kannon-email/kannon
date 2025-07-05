package cmd

import (
	"context"

	"github.com/ludusrusso/kannon/internal/x/container"
	"github.com/ludusrusso/kannon/pkg/api"
	"github.com/ludusrusso/kannon/pkg/bump"
	"github.com/ludusrusso/kannon/pkg/dispatcher"
	"github.com/ludusrusso/kannon/pkg/sender"
	"github.com/ludusrusso/kannon/pkg/smtp"
	"github.com/ludusrusso/kannon/pkg/stats"
	"github.com/ludusrusso/kannon/pkg/validator"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

const envPrefix = "K"

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

	dbUrl := viper.GetString("database_url")
	natsUrl := viper.GetString("nats_url")

	cnt := container.New(ctx, container.Config{
		DBUrl:   dbUrl,
		NatsURL: natsUrl,
	})
	defer cnt.Close()

	config, err := readConfig()
	if err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	if config.RunSender {
		g.Go(func() error {
			return runSender(ctx, cnt, config)
		})
	}

	if config.RunDispatcher {
		g.Go(func() error {
			return runDispatcher(ctx, cnt, config)
		})
	}

	if config.RunVerifier {
		g.Go(func() error {
			if err := validator.Run(ctx); err != nil {
				logrus.Fatalf("error in verifier: %v", err)
			}
			return nil
		})
	}

	if config.RunStats {
		g.Go(func() error {
			stats.Run(ctx, cnt)
			return nil
		})
	}

	if config.RunBounce {
		g.Go(func() error {
			bump.Run(ctx, cnt)
			return nil
		})
	}

	if config.RunAPI {
		g.Go(func() error {
			api.Run(ctx, api.Config{
				Port: config.API.Port,
			}, cnt)
			return nil
		})
	}

	if config.RunSMTP {
		g.Go(func() error {
			return runSMTP(ctx, cnt, config)
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

func createBoolFlagAndBindToViper(name string, value bool, usage string) {
	rootCmd.PersistentFlags().Bool(name, value, usage)
	err := viper.BindPFlag(name, rootCmd.PersistentFlags().Lookup(name))
	if err != nil {
		logrus.Fatalf("cannot set flat '%v': %v", name, err)
	}
}

func runDispatcher(ctx context.Context, cnt *container.Container, config Config) error {
	return dispatcher.Run(ctx, cnt)
}

func runSender(ctx context.Context, cnt *container.Container, config Config) error {
	cnf := config.Sender.ToSenderConfig()
	return sender.Run(ctx, cnt, cnf)
}

func runSMTP(ctx context.Context, cnt *container.Container, config Config) error {
	cnf := config.SMTP.ToSMTPConfig()
	return smtp.Run(ctx, cnt, cnf)
}
