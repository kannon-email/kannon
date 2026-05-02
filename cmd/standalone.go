package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var standaloneCmd = &cobra.Command{
	Use:   "standalone",
	Short: "Run Kannon in standalone mode with embedded NATS",
	Long: `Run all Kannon components (API, SMTP, SMTPSender, Dispatcher, Validator, Stats, Tracker)
in a single process with an embedded NATS server. This mode is ideal for development,
testing, or single-server deployments. You will still need a PostgreSQL database.`,
	Run: runStandalone,
}

func init() {
	rootCmd.AddCommand(standaloneCmd)
}

func runStandalone(cmd *cobra.Command, _ []string) {
	if err := readViperConfig(); err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}
	viper.Set("use_embedded_nats", true)
	bootstrap(cmd, RunFlags{
		Sender:     true,
		Dispatcher: true,
		Validator:  true,
		Stats:      true,
		Tracker:    true,
		API:        true,
		SMTP:       true,
	})
}
