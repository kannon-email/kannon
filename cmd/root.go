package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/kannon-email/kannon/x/container"
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

func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, _ []string) {
	if err := readViperConfig(); err != nil {
		slog.Error("error in reading config", "err", err)
		os.Exit(1)
	}
	if err := bootstrap(cmd, runFlagsFromViper()); err != nil {
		slog.Error("service error", "err", err)
		os.Exit(1)
	}
}

// bootstrap is the single boot path shared by `kannon` and `kannon standalone`.
// It builds the container, registers every runnable selected by flags, and
// starts them under a shared errgroup-derived context.
func bootstrap(cmd *cobra.Command, flags RunFlags) error {
	ctx := cmd.Context()

	// Before anything starts, since this is a deployment mistake rather than a request-time
	// refusal: a process serving the Admin and Stats APIs without their credential would come
	// up healthy and answer every request with unauthenticated. It stops the whole boot — the
	// workers included — because a Kannon half up hides the reason the API is unusable.
	if err := requireAdminToken(flags); err != nil {
		return err
	}

	cnt := container.New(ctx)
	defer func() {
		if err := cnt.CloseWithTimeout(shutdownTimeout); err != nil {
			slog.Error("Shutdown errors", "err", err)
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

	slog.Info(fmt.Sprintf("Starting Kannon runnables: %v", reg.Names()))

	return reg.Run(ctx)
}

// requireAdminToken refuses a boot that would serve the Admin and Stats APIs with no credential to
// authenticate them. Gated on the API runnable rather than demanded of every process: a deployment
// running only the dispatcher serves none of those endpoints, and making it carry an admin secret
// would put the credential on hosts that have no use for it.
func requireAdminToken(flags RunFlags) error {
	if !flags.API {
		return nil
	}
	_, err := api.AdminToken()
	return err
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
		slog.Error("cannot set flat", "name", name, "err", err)
		os.Exit(1)
	}
}
