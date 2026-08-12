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
// That replaced the K_ prefix, which viper could only apply to the handful of
// top-level keys bound by hand and silently ignored everywhere else — see
// x/config/envref. The prefix is gone (ADR 0012); Kannon warns at boot about any
// K_ variable it finds, since it no longer reads one.
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

// Prepare points viper at the configuration file. Called before any command runs,
// and separate from Read because the --config flag it depends on is only parsed by
// then.
func Prepare(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigType("yaml")
		viper.SetConfigName(defaultConfigName)
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(home)
		} else {
			// Not fatal — every setting can come from the environment — but not
			// quiet either: this branch is only reached when no --config was
			// given, so the process was meant to read a file and will now run on
			// whatever the environment happens to hold. Warn and not Debug
			// because Prepare runs before the file that could ask for debug
			// logging has been read, so a Debug line here could never be seen.
			slog.Warn("cannot locate the home directory and no --config was given, so no configuration file will be read", "err", err)
		}
	}
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

	// After the file has been read, so that a variable the file names can be told
	// from one left behind in a manifest, and before anything reads a key: the
	// settings this warning is about are the ones the unmarshal below is about to
	// find missing.
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
//
// Only ever call this while the boot path is still running — from a runnable's
// constructor, not from its Run — or the panic lands in a goroutine nobody
// recovers and takes the whole process with it. TryLoadSection is for a reader
// that has somewhere better to go.
func LoadSection(key string, out any) {
	if err := TryLoadSection(key, out); err != nil {
		panic(err)
	}
}

// TryLoadSection is LoadSection for a caller that must not fail on the operator's
// mistake: a section belonging to a feature whose absence is not an outage, or one
// read after the boot path is over. It stands to LoadSection as the container's
// TryNats stands to Nats.
func TryLoadSection(key string, out any) error {
	if err := viper.UnmarshalKey(key, out, envref.Decoder()); err != nil {
		return fmt.Errorf("config: failed to load config %q: %w", key, err)
	}
	return nil
}

// apiKey is the API runnable's section, named here because the admin token below
// is read out of it and the two spellings must not drift.
const apiKey = "api"

// APIAdminTokenKey holds the credential that authenticates the Admin API and both Stats API
// versions (ADR 0009). Exported because the operator has to be told which key to set when it is
// missing, and a message naming a key the code does not read is worse than no message.
const APIAdminTokenKey = apiKey + ".admin_token"

// APIAdminToken returns the configured admin credential, empty when none is set — the caller
// decides what that means, which for the API runnable is a refusal to boot.
//
// Read through the section, like every other key, so the reference an operator writes here is
// resolved by the same decode hook as everything else. It used to be the one value read with
// viper.GetString and resolved by hand — which meant the one credential in the system was also the
// one value the mechanism did not validate — and then the one key the removed K_ prefix reached
// below the top level.
func APIAdminToken() (string, error) {
	var api struct {
		AdminToken string `mapstructure:"admin_token"`
	}
	if err := TryLoadSection(apiKey, &api); err != nil {
		return "", err
	}
	return api.AdminToken, nil
}

// errorOnUnknownKeys refuses a section carrying a key Kannon does not know. Off by default in
// mapstructure, and left off for the sections a runnable reads — an operator whose file predates a
// removed knob should not be stopped by it — but demanded of `services`, where a misspelling is the
// difference between a process that works and one that starts nothing at all.
func errorOnUnknownKeys(c *mapstructure.DecoderConfig) {
	c.ErrorUnused = true
}
