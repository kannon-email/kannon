package cmd

import (
	"testing"

	"github.com/spf13/viper"
)

// Regression: --run-bounce / K_RUN_BOUNCE must promote to run-tracker before
// runFlagsFromViper reads it. Otherwise the deprecated alias fires too late,
// the registry is empty, and the process exits immediately after logging
// "Starting Kannon runnables: []".
func TestReadViperConfig_PromotesRunBounceBeforeFlagsAreRead(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("run-bounce", true)

	if err := readViperConfig(); err != nil {
		t.Fatalf("readViperConfig: %v", err)
	}

	flags := runFlagsFromViper()
	if !flags.Tracker {
		t.Errorf("expected Tracker=true after run-bounce alias promotion, got %+v", flags)
	}
}

func TestReadViperConfig_PromotesRunVerifierBeforeFlagsAreRead(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("run-verifier", true)

	if err := readViperConfig(); err != nil {
		t.Fatalf("readViperConfig: %v", err)
	}

	flags := runFlagsFromViper()
	if !flags.Validator {
		t.Errorf("expected Validator=true after run-verifier alias promotion, got %+v", flags)
	}
}
