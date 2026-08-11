// Package config reads Kannon's configuration: it locates the file, resolves the
// `env://` references an operator wrote into it, and hands each runnable its own
// section.
//
// The file is the contract. Every setting can be written there, and any of them
// can be taken from the environment by naming the variable it should come from:
//
//	database_url: env://KANNON_DATABASE_URL
//	api:
//	  admin_token: env://KANNON_ADMIN_TOKEN
//	services:
//	  stats:
//	    enabled: env://KANNON_ENABLE_STATS:-false
//
// That replaces the K_ prefix, which viper could only apply to the handful of
// top-level keys bound by hand and silently ignored everywhere else — see
// x/config/envref. The prefix keeps working, deprecated, so an existing
// deployment can migrate one variable at a time.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/go-viper/mapstructure/v2"
	"github.com/kannon-email/kannon/x/config/envref"
	"github.com/spf13/viper"
)

// defaultConfigName is the file kannon reads from the home directory when no
// --config is given, without its extension.
const defaultConfigName = ".kannon"

// RootConfig is the configuration belonging to no single runnable: the two
// connections every Kannon process may make, and how loudly it logs.
type RootConfig struct {
	DatabaseURL     string `mapstructure:"database_url"`
	NatsURL         string `mapstructure:"nats_url"`
	UseEmbeddedNats bool   `mapstructure:"use_embedded_nats"`
	Debug           bool   `mapstructure:"debug"`
}

// Prepare points viper at the configuration file and installs the deprecated
// environment prefix. Called before any command runs, and separate from Read
// because the --config flag it depends on is only parsed by then.
func Prepare(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigType("yaml")
		viper.SetConfigName(defaultConfigName)
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(home)
		} else {
			// Not fatal: it only means there is no default file to find, and
			// a deployment passing --config never wanted one.
			slog.Debug("cannot locate the home directory, so no default config file will be read", "err", err)
		}
	}

	prepareLegacyEnv()
}

// Read loads the configuration file and returns the settings that belong to no
// runnable in particular. A missing file is not an error — every setting can come
// from the environment instead — but an unreadable one is, and so is a reference
// to a variable nobody set.
//
// It returns an error rather than panicking the way LoadSection does, because it
// runs before anything is registered: this is the last point at which an
// operator's mistake can be reported as a message rather than as a stack trace.
func Read() (RootConfig, error) {
	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return RootConfig{}, fmt.Errorf("cannot read config file: %w", err)
		}
	}

	// Before anything reads a key: a deprecated spelling has to have been
	// promoted onto its canonical name by the time the first unmarshal runs.
	ApplyDeprecatedAliases()
	warnLegacyEnvPrefix()

	var cfg RootConfig
	if err := viper.Unmarshal(&cfg, envref.Decoder()); err != nil {
		return RootConfig{}, fmt.Errorf("cannot resolve the configuration: %w", err)
	}

	return cfg, nil
}

// LoadSection unmarshals the viper sub-tree at key into out, resolving the
// `env://` references in it, and panicking on failure with a message naming the
// key. This is how runnables read configuration, and a malformed value means the
// operator's file is wrong, which must surface at boot rather than as zero
// values.
func LoadSection(key string, out any) {
	if err := viper.UnmarshalKey(key, out, envref.Decoder()); err != nil {
		panic(fmt.Errorf("config: failed to load config %q: %w", key, err))
	}
}

// APIAdminTokenKey holds the credential that authenticates the Admin API and both Stats API
// versions (ADR 0009). Exported because the operator has to be told which key to set when it is
// missing, and a message naming a key the code does not read is worse than no message.
const APIAdminTokenKey = "api.admin_token"

// APIAdminTokenEnvVar is the deprecated K_ spelling of the same key, kept because it is what the
// Kubernetes manifests of every existing deployment carry. The replacement is to name a variable
// in the file — `admin_token: env://ANYTHING` — which is no longer a special case for this key.
const APIAdminTokenEnvVar = legacyEnvPrefix + "_API_ADMIN_TOKEN"

// APIAdminToken returns the configured admin credential, empty when none is set — the caller
// decides what that means, which for the API runnable is a refusal to boot.
//
// Read key by key rather than through the "api" section's struct, which is the one place left doing
// so. Two reasons, and they pull the same way: viper's Get consults the environment for a nested key
// where UnmarshalKey does not, which is what keeps K_API_ADMIN_TOKEN working; and setting a nested
// key through viper.Set would hide the rest of the section from UnmarshalKey, so a token promoted
// that way would cost the operator their api.port. The reference in the file is resolved here by
// hand for the same reason — the decode hook only runs while something is being decoded.
func APIAdminToken() (string, error) {
	// Bound here rather than in prepareLegacyEnv, and with the variable named rather than derived,
	// so that this key answers the same in a process that only ever set an environment variable —
	// which every deployment of Kannon written before the file could name its own variables is.
	//nolint:errcheck
	viper.BindEnv(APIAdminTokenKey, APIAdminTokenEnvVar)
	return envref.Resolve(viper.GetString(APIAdminTokenKey))
}

// errorOnUnknownKeys refuses a section carrying a key Kannon does not know. Off by default in
// mapstructure, and left off for the sections a runnable reads — an operator whose file predates a
// removed knob should not be stopped by it — but demanded of `services`, where a misspelling is the
// difference between a process that works and one that starts nothing at all.
func errorOnUnknownKeys(c *mapstructure.DecoderConfig) {
	c.ErrorUnused = true
}
