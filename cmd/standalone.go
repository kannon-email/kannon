package cmd

import (
	"log/slog"
	"os"

	"github.com/kannon-email/kannon/x/startup"
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
	if err := serve(cmd.Context(), startup.Standalone()); err != nil {
		slog.Error("service error", "err", err)
		os.Exit(1)
	}
}
