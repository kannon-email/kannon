/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

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
		config, err := readConfig()
		if err != nil {
			logrus.Fatalf("error in reading config: %v", err)
		}

		fmt.Println("Viper database_url:", viper.GetString("database_url"))
		logrus.Infof("migrating database to last version %s", config.DatabaseURL)
		err = dbschema.Migrate(config.DatabaseURL)
		if err != nil {
			logrus.Fatalf("error in migration: %v", err)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateMainCmd)
}
