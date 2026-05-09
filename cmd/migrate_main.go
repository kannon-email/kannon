/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"log/slog"
	"os"

	dbschema "github.com/kannon-email/kannon/db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// mainCmd represents the main command
var migrateMainCmd = &cobra.Command{
	Use:   "main",
	Short: "Migrate Main Database to last version",
	Run: func(_ *cobra.Command, _ []string) {
		if err := readViperConfig(); err != nil {
			slog.Error("error in reading config", "err", err)
			os.Exit(1)
		}

		if err := dbschema.Migrate(viper.GetString("database_url")); err != nil {
			slog.Error("error in migration", "err", err)
			os.Exit(1)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateMainCmd)
}
