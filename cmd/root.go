package cmd

import (
	"time"

	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/kannon-email/kannon/x/container"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

const shutdownTimeout = 30 * time.Second

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, _ []string) {
	if err := readViperConfig(); err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}
	bootstrap(cmd, runFlagsFromViper())
}

// bootstrap is the single boot path shared by `kannon` and `kannon standalone`.
// It builds the container, registers every runnable selected by flags, and
// starts them under a shared errgroup-derived context.
func bootstrap(cmd *cobra.Command, flags RunFlags) {
	ctx := cmd.Context()

	cnt := container.New(ctx)
	defer func() {
		if err := cnt.CloseWithTimeout(shutdownTimeout); err != nil {
			logrus.Errorf("Shutdown errors: %v", err)
		}
	}()

	reg := &container.Registry{}

	if flags.Sender {
		reg.Register(smtpsender.New(cnt))
	}
	if flags.Dispatcher {
		reg.Register(dispatcher.New(cnt))
	}
	if flags.Validator {
		reg.Register(validator.New(cnt))
	}
	if flags.Stats {
		reg.Register(stats.New(cnt))
	}
	if flags.Tracker {
		reg.Register(tracker.New(cnt))
	}
	if flags.API {
		reg.Register(api.New(cnt))
	}
	if flags.SMTP {
		reg.Register(smtp.New(cnt))
	}

	logrus.Infof("Starting Kannon runnables: %v", reg.Names())

	if err := reg.Run(ctx); err != nil {
		logrus.Fatalf("service error: %v", err)
	}
}

func init() {
	cobra.OnInitialize(prepareConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
	rootCmd.PersistentFlags().Bool("viper", true, "use Viper for configuration")
	createBoolFlagAndBindToViper("run-sender", false, "run sender")
	createBoolFlagAndBindToViper("run-dispatcher", false, "run dispatcher")
	createBoolFlagAndBindToViper("run-validator", false, "run validator")
	createBoolFlagAndBindToViper("run-verifier", false, "DEPRECATED: use --run-validator")
	createBoolFlagAndBindToViper("run-tracker", false, "run tracker")
	createBoolFlagAndBindToViper("run-bounce", false, "DEPRECATED: use --run-tracker")
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
