package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestApplyDeprecatedAliases_RunVerifier(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	buf := captureSlog(t)

	viper.Set("run-verifier", true)

	ApplyDeprecatedAliases()

	if !viper.GetBool("run-validator") {
		t.Error("expected run-validator to be promoted")
	}
	if !strings.Contains(buf.String(), "run-verifier") || !strings.Contains(buf.String(), "deprecated") {
		t.Errorf("expected deprecation warning for run-verifier, got %q", buf.String())
	}
}

func TestApplyDeprecatedAliases_BumpPort(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	buf := captureSlog(t)

	viper.Set("bump.port", 1234)

	ApplyDeprecatedAliases()

	if got := viper.GetInt("tracker.port"); got != 1234 {
		t.Errorf("expected tracker.port=1234, got %d", got)
	}
	if !strings.Contains(buf.String(), "bump") || !strings.Contains(buf.String(), "deprecated") {
		t.Errorf("expected deprecation warning for bump section, got %q", buf.String())
	}
}

func TestApplyDeprecatedAliases_BumpPortDoesNotOverrideTracker(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("bump.port", 1234)
	viper.Set("tracker.port", 5678)

	ApplyDeprecatedAliases()

	if got := viper.GetInt("tracker.port"); got != 5678 {
		t.Errorf("expected tracker.port to remain 5678, got %d", got)
	}
}

func TestApplyDeprecatedAliases_NoOpWhenUnset(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	ApplyDeprecatedAliases()

	if viper.GetBool("run-validator") {
		t.Error("run-validator should not be set")
	}
	if viper.IsSet("tracker.port") {
		t.Error("tracker.port should not be set")
	}
}

// Every K_ variable is named, not only the ones Kannon reads: a deployment
// carrying K_TRACKER_PORT has been setting nothing for as long as it has existed,
// and this line is the only way its operator finds out.
func TestWarnLegacyEnvPrefix(t *testing.T) {
	prev := environ
	t.Cleanup(func() { environ = prev })
	environ = func() []string {
		return []string{"PATH=/usr/bin", "K_TRACKER_PORT=8080", "K_DATABASE_URL=postgres://db", "KANNON_ENABLE_API=true"}
	}

	buf := captureSlog(t)
	warnLegacyEnvPrefix()

	logged := buf.String()
	for _, want := range []string{"deprecated", "K_DATABASE_URL", "K_TRACKER_PORT", "env://"} {
		if !strings.Contains(logged, want) {
			t.Errorf("expected the warning to mention %q, got %q", want, logged)
		}
	}
	if strings.Contains(logged, "KANNON_ENABLE_API") {
		t.Errorf("a variable outside the prefix is not deprecated, got %q", logged)
	}
}

func TestWarnLegacyEnvPrefix_SilentWithoutTheLegacyPrefix(t *testing.T) {
	prev := environ
	t.Cleanup(func() { environ = prev })
	environ = func() []string { return []string{"PATH=/usr/bin", "KANNON_DATABASE_URL=postgres://db"} }

	buf := captureSlog(t)
	warnLegacyEnvPrefix()

	if buf.Len() != 0 {
		t.Errorf("expected no warning, got %q", buf.String())
	}
}

// The flag's replacement is named in the terms an operator has to type, since the
// message pflag prints is all they get.
func TestDeprecatedRunFlagMessage(t *testing.T) {
	got := DeprecatedRunFlagMessage("stats")
	for _, want := range []string{"services", "stats", "enabled"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to mention %q", got, want)
		}
	}
}
