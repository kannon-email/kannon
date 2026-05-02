/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	dbschema "github.com/kannon-email/kannon/db"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// mainCmd represents the main command
var migrateMainCmd = &cobra.Command{
	Use:   "main",
	Short: "Migrate Main Database to last version",
	Run: func(_ *cobra.Command, _ []string) {
		if err := readViperConfig(); err != nil {
			logrus.Fatalf("error in reading config: %v", err)
		}

		if err := dbschema.Migrate(viper.GetString("database_url")); err != nil {
			logrus.Fatalf("error in migration: %v", err)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateMainCmd)
}
