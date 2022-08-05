package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ludusrusso/kannon/pkg/api"
	"github.com/ludusrusso/kannon/pkg/bounce"
	"github.com/ludusrusso/kannon/pkg/dispatcher"
	"github.com/ludusrusso/kannon/pkg/sender"
	"github.com/ludusrusso/kannon/pkg/stats"
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
			bounce.Run(ctx)
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

	wg.Wait()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kannon.yaml)")
	rootCmd.PersistentFlags().StringP("author", "a", "YOUR NAME", "author name for copyright attribution")
	rootCmd.PersistentFlags().Bool("viper", true, "use Viper for configuration")
	createBoolFlagAndBindToViper("run-sender", false, "run sender")
	createBoolFlagAndBindToViper("run-dispatcher", false, "run dispatcher")
	createBoolFlagAndBindToViper("run-bounce", false, "run bounce")
	createBoolFlagAndBindToViper("run-stats", false, "run stats")
	createBoolFlagAndBindToViper("run-api", false, "run api")
	createBoolFlagAndBindToViper("run-smtp", false, "run smtp server")
}

func createBoolFlagAndBindToViper(name string, value bool, usage string) {
	rootCmd.PersistentFlags().Bool(name, value, usage)
	viper.BindPFlag(name, rootCmd.PersistentFlags().Lookup(name))
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
}
