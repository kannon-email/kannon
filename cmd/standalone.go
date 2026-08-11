package cmd

import (
	"log/slog"
	"os"

	"github.com/kannon-email/kannon/x/config"
	"github.com/spf13/cobra"
)

var standaloneCmd = &cobra.Command{
	Use:   "standalone",
	Short: "Run Kannon in standalone mode with embedded NATS",
	Long: `Run all Kannon components (API, SMTP, SMTPSender, Dispatcher, Validator, Stats,
Tracker, Audit) in a single process with an embedded NATS server. This mode is ideal for
development, testing, or single-server deployments. You will still need a PostgreSQL
database. The audit writer starts only once audit.enabled is set, since nothing publishes
authorization decisions until it is.`,
	Run: runStandalone,
}

func init() {
	rootCmd.AddCommand(standaloneCmd)
}

func runStandalone(cmd *cobra.Command, _ []string) {
	cfg, err := readConfig()
	if err != nil {
		slog.Error("error in reading config", "err", err)
		os.Exit(1)
	}

	// Overridden on the loaded configuration rather than pushed back into viper:
	// this is what the subcommand means, not something an operator configured.
	cfg.UseEmbeddedNats = true

	if err := bootstrap(cmd, cfg, config.AllServices()); err != nil {
		slog.Error("service error", "err", err)
		os.Exit(1)
	}
}
