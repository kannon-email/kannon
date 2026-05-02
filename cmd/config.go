package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// RunFlags collects the seven boolean flags that select which runnables
// kannon starts. It is driven by cobra flags, never viper-loaded — config
// belongs to each runnable's package, not the cmd layer.
type RunFlags struct {
	Sender     bool
	Dispatcher bool
	Validator  bool
	Stats      bool
	Tracker    bool
	API        bool
	SMTP       bool
}

const envPrefix = "K"

func prepareConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kannon")
	}

	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()
}

func readViperConfig() error {
	//nolint:errcheck
	viper.BindEnv("database_url")
	//nolint:errcheck
	viper.BindEnv("nats_url")
	//nolint:errcheck
	viper.BindEnv("debug")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("cannot read config file: %v", err)
		}
	}

	if viper.GetBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
	}

	return nil
}

func runFlagsFromViper() RunFlags {
	return RunFlags{
		Sender:     viper.GetBool("run-sender"),
		Dispatcher: viper.GetBool("run-dispatcher"),
		Validator:  viper.GetBool("run-validator"),
		Stats:      viper.GetBool("run-stats"),
		Tracker:    viper.GetBool("run-tracker"),
		API:        viper.GetBool("run-api"),
		SMTP:       viper.GetBool("run-smtp"),
	}
}
