package cmd

import (
	"log/slog"
	"os"

	"github.com/kannon-email/kannon/x/config"
)

// logLevel is the dynamic level for the slog default handler installed at
// package init. readConfig flips it to Debug when the debug key is set.
var logLevel = new(slog.LevelVar)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
}

// readConfig loads the configuration file and applies the one setting the cmd
// layer owns: how loudly the process logs. Everything else in it belongs to a
// runnable's own package, and is read there.
func readConfig() (config.RootConfig, error) {
	cfg, err := config.Read()
	if err != nil {
		return config.RootConfig{}, err
	}

	if cfg.Debug {
		logLevel.Set(slog.LevelDebug)
	}

	return cfg, nil
}

// readConfigAndServices is readConfig plus the section saying which components this
// process is, for the commands that start components.
//
// The two live in one function because their order matters and is the cmd layer's
// to get wrong: config.Read is what promotes the deprecated spellings, and two of
// them — --run-bounce and --run-verifier — are keys config.LoadServices then reads.
// Read the services first and those aliases are promoted too late, the registry
// comes up empty, and the process exits as if it had been asked to run nothing.
func readConfigAndServices() (config.RootConfig, config.Services, error) {
	cfg, err := readConfig()
	if err != nil {
		return config.RootConfig{}, config.Services{}, err
	}

	services, err := config.LoadServices()
	if err != nil {
		return config.RootConfig{}, config.Services{}, err
	}

	return cfg, services, nil
}
