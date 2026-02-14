/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	dbschema "github.com/kannon-email/kannon/db"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// mainCmd represents the main command
var migrateMainCmd = &cobra.Command{
	Use:   "main",
	Short: "Migrate Main Database to last version",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := readConfig()
		if err != nil {
			logrus.Fatalf("error in reading config: %v", err)
		}

		err = dbschema.Migrate(config.DatabaseURL)
		if err != nil {
			logrus.Fatalf("error in migration: %v", err)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateMainCmd)
}
