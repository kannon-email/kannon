package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	DatabaseURL   string        `mapstructure:"database_url"`
	NatsURL       string        `mapstructure:"nats_url"`
	Debug         bool          `mapstructure:"debug"`
	API           APIConfig     `mapstructure:"api"`
	Sender        SenderConfig  `mapstructure:"sender"`
	SMTP          SMTPConfig    `mapstructure:"smtp"`
	Tracker       TrackerConfig `mapstructure:"tracker"`
	Stats         StatsConfig   `mapstructure:"stats"`
	RunAPI        bool          `mapstructure:"run-api"`
	RunSMTP       bool          `mapstructure:"run-smtp"`
	RunSender     bool          `mapstructure:"run-sender"`
	RunDispatcher bool          `mapstructure:"run-dispatcher"`
	RunValidator  bool          `mapstructure:"run-validator"`
	RunBounce     bool          `mapstructure:"run-bounce"`
	RunStats      bool          `mapstructure:"run-stats"`
	ConfigFile    string        `mapstructure:"config"`
}

type APIConfig struct {
	Port uint `mapstructure:"port"`
}

func (c APIConfig) ToAPIConfig() api.Config {
	return api.Config{
		Port: c.Port,
	}
}

type TrackerConfig struct {
	Port uint `mapstructure:"port"`
}

func (c TrackerConfig) ToTrackerConfig() tracker.Config {
	return tracker.Config{
		Port: c.Port,
	}
}

type SenderConfig struct {
	Hostname   string `mapstructure:"hostname"`
	MaxJobs    uint   `mapstructure:"max_jobs"`
	DemoSender bool   `mapstructure:"demo_sender"`
}

func (c SenderConfig) ToSenderConfig() smtpsender.Config {
	return smtpsender.Config{
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
	retention := max(c.Retention, 8760*time.Hour) // 1 year minimum
	return stats.Config{
		Retention: retention,
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

	applyDeprecatedAliases()

	if err := viper.Unmarshal(&config); err != nil {
		return Config{}, fmt.Errorf("unable to decode config into struct: %v", err)
	}

	if config.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	return config, nil
}

// applyDeprecatedAliases promotes deprecated config keys onto their canonical
// names and logs a one-line deprecation warning at startup. Each entry is a
// public API surface we still owe users.
func applyDeprecatedAliases() {
	boolAliases := []struct {
		oldKey string
		newKey string
	}{
		{oldKey: "run-verifier", newKey: "run-validator"},
	}

	for _, a := range boolAliases {
		if !viper.GetBool(a.oldKey) {
			continue
		}
		logrus.Warnf("config key %q is deprecated and will be removed in a future major version; use %q instead", a.oldKey, a.newKey)
		viper.Set(a.newKey, true)
	}

	subKeyAliases := []struct {
		oldKey string
		newKey string
	}{
		{oldKey: "bump.port", newKey: "tracker.port"},
	}

	warnedSections := map[string]bool{}
	for _, a := range subKeyAliases {
		//nolint:errcheck
		viper.BindEnv(a.oldKey)
		if !viper.IsSet(a.oldKey) {
			continue
		}
		oldSection := strings.SplitN(a.oldKey, ".", 2)[0]
		newSection := strings.SplitN(a.newKey, ".", 2)[0]
		if !warnedSections[oldSection] {
			logrus.Warnf("config section %q is deprecated and will be removed in a future major version; use %q instead", oldSection, newSection)
			warnedSections[oldSection] = true
		}
		if !viper.IsSet(a.newKey) {
			viper.Set(a.newKey, viper.Get(a.oldKey))
		}
	}
}
