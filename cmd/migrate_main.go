/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	dbschema "github.com/ludusrusso/kannon/db"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// mainCmd represents the main command
var migrateMainCmd = &cobra.Command{
	Use:   "main",
	Short: "Migrate Main Database to last version",
	Run: func(cmd *cobra.Command, args []string) {
		dbURL := viper.GetString("database_url")
		err := dbschema.Migrate(dbURL)
		if err != nil {
			logrus.Infof("error in migration: %v", err)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateMainCmd)
}
