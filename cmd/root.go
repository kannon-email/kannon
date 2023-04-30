package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ludusrusso/kannon/pkg/api"
	"github.com/ludusrusso/kannon/pkg/bump"
	"github.com/ludusrusso/kannon/pkg/dispatcher"
	"github.com/ludusrusso/kannon/pkg/sender"
	"github.com/ludusrusso/kannon/pkg/smtp"
	"github.com/ludusrusso/kannon/pkg/stats"
	"github.com/ludusrusso/kannon/pkg/validator"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const envPrefix = "K"

var (
	cfgFile string

	rootCmd = &cobra.Command{
		Use:   "kannon",
		Short: "A massive send mail tool for kubernetes",
		Long:  `Kannon is an open source tool for sending massive emails on a kubernetes environment`,
		Run:   run,
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) {
	var wg sync.WaitGroup
	ctx := cmd.Context()

	if viper.GetBool("run-sender") {
		wg.Add(1)
		go func() {
			sender.Run(cmd.Context())
			wg.Done()
		}()
	}

	if viper.GetBool("run-dispatcher") {
		wg.Add(1)
		go func() {
			dispatcher.Run(ctx)
			wg.Done()
		}()
	}

	if viper.GetBool("run-verifier") {
		wg.Add(1)
		go func() {
			if err := validator.Run(ctx); err != nil {
				logrus.Fatalf("error in verifier: %v", err)
			}
			wg.Done()
		}()
	}

	if viper.GetBool("run-stats") {
		wg.Add(1)
		go func() {
			stats.Run(ctx)
			wg.Done()
		}()
	}

	if viper.GetBool("run-bounce") {
		wg.Add(1)
		go func() {
			bump.Run(ctx)
			wg.Done()
		}()
	}

	if viper.GetBool("run-api") {
		wg.Add(1)
		go func() {
			api.Run(ctx)
			wg.Done()
		}()
	}

	if viper.GetBool("run-smtp") {
		wg.Add(1)
		go func() {
			smtp.Run(ctx)
			wg.Done()
		}()
	}

	wg.Wait()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
	rootCmd.PersistentFlags().Bool("viper", true, "use Viper for configuration")
	createBoolFlagAndBindToViper("run-sender", false, "run sender")
	createBoolFlagAndBindToViper("run-dispatcher", false, "run dispatcher")
	createBoolFlagAndBindToViper("run-verifier", false, "run verifier")
	createBoolFlagAndBindToViper("run-bounce", false, "run bounce")
	createBoolFlagAndBindToViper("run-stats", false, "run stats")
	createBoolFlagAndBindToViper("run-api", false, "run api")
	createBoolFlagAndBindToViper("run-smtp", false, "run smtp server")
}

//nolint:unparam
func createBoolFlagAndBindToViper(name string, value bool, usage string) {
	rootCmd.PersistentFlags().Bool(name, value, usage)
	err := viper.BindPFlag(name, rootCmd.PersistentFlags().Lookup(name))
	if err != nil {
		logrus.Fatalf("cannot set flat '%v': %v", name, err)
	}
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".cobra" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kannon")
	}

	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}

	if viper.GetBool("debug") {
		logrus.Infof("setting deubg mode")
		logrus.SetLevel(logrus.DebugLevel)
	}
}
