package cmd

import (
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

	config, err := readConfig()
	if err != nil {
		logrus.Fatalf("error in reading config: %v", err)
	}

	if config.RunSender {
		wg.Add(1)
		go func() {
			cnf := sender.Config{
				Hostname: config.Sender.Hostname,
				MaxJobs:  config.Sender.MaxJobs,
			}
			sender.Run(ctx, cnf)
			wg.Done()
		}()
	}

	if config.RunDispatcher {
		wg.Add(1)
		go func() {
			dispatcher.Run(ctx)
			wg.Done()
		}()
	}

	if config.RunVerifier {
		wg.Add(1)
		go func() {
			if err := validator.Run(ctx); err != nil {
				logrus.Fatalf("error in verifier: %v", err)
			}
			wg.Done()
		}()
	}

	if config.RunStats {
		wg.Add(1)
		go func() {
			stats.Run(ctx)
			wg.Done()
		}()
	}

	if config.RunBounce {
		wg.Add(1)
		go func() {
			bump.Run(ctx)
			wg.Done()
		}()
	}

	if config.RunAPI {
		wg.Add(1)
		go func() {
			api.Run(ctx)
			wg.Done()
		}()
	}

	if config.RunSMTP {
		wg.Add(1)
		go func() {
			cnf := smtp.Config{
				Address:         config.SMTP.Address,
				Domain:          config.SMTP.Domain,
				ReadTimeout:     config.SMTP.ReadTimeout,
				WriteTimeout:    config.SMTP.WriteTimeout,
				MaxPayloadBytes: config.SMTP.MaxPayloadBytes,
				MaxRecipients:   config.SMTP.MaxRecipients,
			}
			smtp.Run(ctx, cnf)
			wg.Done()
		}()
	}

	wg.Wait()
}

func init() {
	cobra.OnInitialize(prepareConfig)

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
