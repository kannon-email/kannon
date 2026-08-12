package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kannon-email/kannon/pkg/api"
	kaudit "github.com/kannon-email/kannon/pkg/audit"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/spf13/cobra"
)

var (
	cfgFile string

	rootCmd = &cobra.Command{
		Use:   "kannon",
		Short: "A massive send mail tool for kubernetes",
		Long: `Kannon is an open source tool for sending massive emails on a kubernetes environment.

Which components a process runs is written in the config file, so that one file can
describe a whole installation and each deployment enable only its own:

  services:
    api:
      enabled: true
    stats:
      enabled: env://KANNON_ENABLE_STATS:-false

The components are sender, dispatcher, validator, stats, tracker, api, smtp and
audit; none of them runs unless it is enabled. Any value in the file can be taken
from the environment as env://NAME, or env://NAME:-default to supply a fallback.`,
		Run: run,
	}
)

const shutdownTimeout = 30 * time.Second

func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, _ []string) {
	cfg, services, err := readConfigAndServices()
	if err != nil {
		slog.Error("error in reading config", "err", err)
		os.Exit(1)
	}

	if err := bootstrap(cmd, cfg, services); err != nil {
		slog.Error("service error", "err", err)
		os.Exit(1)
	}
}

// bootstrap is the single boot path shared by `kannon` and `kannon standalone`.
// It builds the container, registers every runnable the configuration asked for,
// and starts them under a shared errgroup-derived context.
func bootstrap(cmd *cobra.Command, cfg config.RootConfig, services config.Services) error {
	ctx := cmd.Context()

	// Both refusals below are read off the `services` section and answered before anything is
	// built. The container and every runnable read sections of their own, any of which may name a
	// variable only some other pod sets, so a mistake in what this process was asked to be has to
	// be reported before a section belonging to a component it does not even run can panic first.
	//
	// A process with nothing to run used to log "Starting Kannon runnables: []" and exit 0, which
	// reads as a clean shutdown in every dashboard there is. The ways to arrive here are all
	// mistakes: a `services` section nobody filled in, an enabling variable spelled one way in the
	// file and another in the manifest.
	if len(services.Enabled()) == 0 {
		return errors.New("this process was asked to run nothing: enable at least one component " +
			"under `services` in the config file, e.g. `services: {api: {enabled: true}}`")
	}

	// A deployment mistake rather than a request-time refusal: a process serving the Admin and
	// Stats APIs without their credential would come up healthy and answer every request with
	// unauthenticated. It stops the whole boot — the workers included — because a Kannon half up
	// hides the reason the API is unusable.
	if err := requireAdminToken(services); err != nil {
		return err
	}

	cnt := container.New(ctx, cfg)
	defer func() {
		if err := cnt.CloseWithTimeout(shutdownTimeout); err != nil {
			slog.Error("Shutdown errors", "err", err)
		}
	}()

	reg := &container.Registry{}

	if services.Sender.Enabled {
		reg.Register(smtpsender.New(cnt))
	}
	if services.Dispatcher.Enabled {
		reg.Register(dispatcher.New(cnt))
	}
	if services.Validator.Enabled {
		reg.Register(validator.New(cnt))
	}
	if services.Stats.Enabled {
		reg.Register(stats.New(cnt))
	}
	if services.Tracker.Enabled {
		reg.Register(tracker.New(cnt))
	}
	if services.API.Enabled {
		reg.Register(api.New(cnt))
	}
	if services.SMTP.Enabled {
		reg.Register(smtp.New(cnt))
	}
	if services.Audit.Enabled {
		reg.Register(kaudit.New(cnt))
	}

	slog.Info(fmt.Sprintf("Starting Kannon runnables: %v", reg.Names()))

	return reg.Run(ctx)
}

// requireAdminToken refuses a boot that would serve the Admin and Stats APIs with no credential to
// authenticate them. Gated on the API runnable rather than demanded of every process: a deployment
// running only the dispatcher serves none of those endpoints, and making it carry an admin secret
// would put the credential on hosts that have no use for it.
func requireAdminToken(services config.Services) error {
	if !services.API.Enabled {
		return nil
	}
	_, err := api.AdminToken()
	return err
}

func init() {
	cobra.OnInitialize(func() { config.Prepare(cfgFile) })

	// The only flag left: which components a process runs is written in the config file
	// (ADR 0012), and everything else in it is a key rather than an argument. A --run-*
	// flag left in a manifest is now refused by cobra as an unknown flag, which is the
	// loudest way an installation that has not migrated can be told.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
}
