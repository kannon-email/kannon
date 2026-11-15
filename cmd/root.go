package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kannon-email/kannon/x/startup"
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

func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, _ []string) {
	if err := serve(cmd.Context()); err != nil {
		slog.Error("service error", "err", err)
		os.Exit(1)
	}
}

// serve starts Kannon and runs it until a signal asks it to stop or a component fails.
func serve(ctx context.Context, opts ...startup.Option) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	inst, err := startup.Start(ctx, cfgFile, append(opts, startup.WithLogLevel(logLevel))...)
	if err != nil {
		return err
	}
	runErr := inst.Wait()
	if err := inst.Close(); err != nil {
		slog.Error("Shutdown errors", "err", err)
	}
	return runErr
}

func init() {
	// The only flag there is: which components a process runs, and every setting they
	// read, is written in the config file, so the command line has nothing left to say
	// beyond where that file is.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
}
