package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/bump"
	"github.com/kannon-email/kannon/pkg/sender"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	DatabaseURL   string       `mapstructure:"database_url"`
	NatsURL       string       `mapstructure:"nats_url"`
	Debug         bool         `mapstructure:"debug"`
	API           APIConfig    `mapstructure:"api"`
	Sender        SenderConfig `mapstructure:"sender"`
	SMTP          SMTPConfig   `mapstructure:"smtp"`
	Bump          BumpConfig   `mapstructure:"bump"`
	Stats         StatsConfig  `mapstructure:"stats"`
	RunAPI        bool         `mapstructure:"run-api"`
	RunSMTP       bool         `mapstructure:"run-smtp"`
	RunSender     bool         `mapstructure:"run-sender"`
	RunDispatcher bool         `mapstructure:"run-dispatcher"`
	RunVerifier   bool         `mapstructure:"run-verifier"`
	RunBounce     bool         `mapstructure:"run-bounce"`
	RunStats      bool         `mapstructure:"run-stats"`
	ConfigFile    string       `mapstructure:"config"`
}

type APIConfig struct {
	Port uint `mapstructure:"port"`
}

func (c APIConfig) ToAPIConfig() api.Config {
	return api.Config{
		Port: c.Port,
	}
}

type BumpConfig struct {
	Port uint `mapstructure:"port"`
}

func (c BumpConfig) ToBumpConfig() bump.Config {
	return bump.Config{
		Port: c.Port,
	}
}

type SenderConfig struct {
	Hostname   string `mapstructure:"hostname"`
	MaxJobs    uint   `mapstructure:"max_jobs"`
	DemoSender bool   `mapstructure:"demo_sender"`
}

func (c SenderConfig) ToSenderConfig() sender.Config {
	return sender.Config{
		MaxJobs: c.MaxJobs,
	}
}

type SMTPConfig struct {
	Address         string        `mapstructure:"address"`
	Domain          string        `mapstructure:"domain"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	MaxPayloadBytes uint          `mapstructure:"max_payload"`
	MaxRecipients   uint          `mapstructure:"max_recipients"`
}

func (c SMTPConfig) ToSMTPConfig() smtp.Config {
	return smtp.Config{
		Address:         c.Address,
		Domain:          c.Domain,
		ReadTimeout:     c.ReadTimeout,
		WriteTimeout:    c.WriteTimeout,
		MaxPayloadBytes: c.MaxPayloadBytes,
		MaxRecipients:   c.MaxRecipients,
	}
}

type StatsConfig struct {
	Retention time.Duration `mapstructure:"retention"`
}

func (c StatsConfig) ToStatsConfig() stats.Config {
	return stats.Config{
		Retention: c.Retention,
	}
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

func readConfig() (Config, error) {
	//nolint:errcheck
	viper.BindEnv("database_url")
	//nolint:errcheck
	viper.BindEnv("nats_url")
	//nolint:errcheck
	viper.BindEnv("debug")

	viper.SetDefault("stats.retention", "8760h") // 1 year

	var config Config
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("cannot read config file: %v", err)
		}
	}

	if err := viper.Unmarshal(&config); err != nil {
		return Config{}, fmt.Errorf("unable to decode config into struct: %v", err)
	}

	if config.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	return config, nil
}
