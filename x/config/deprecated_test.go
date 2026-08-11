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

// The promotion must not hide the keys beside the one it writes. `tracker` has a
// single key today, which is the only reason a nested viper.Set was survivable
// here: with a sibling in the file, the section read would have seen the promoted
// map alone and lost it.
func TestApplyDeprecatedAliases_BumpPortKeepsTheRestOfTheTrackerSection(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	writeConfig(t, "bump:\n  port: 1234\ntracker:\n  base_url: https://stats.example.com\n")

	if _, err := Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}

	// base_url is not a key tracker has; it stands in for whichever second key it
	// grows first, since that is the day this would otherwise start failing.
	var tracker struct {
		Port    int    `mapstructure:"port"`
		BaseURL string `mapstructure:"base_url"`
	}
	LoadSection("tracker", &tracker)

	if tracker.Port != 1234 {
		t.Errorf("tracker.port = %d, want the promoted bump.port", tracker.Port)
	}
	if tracker.BaseURL != "https://stats.example.com" {
		t.Errorf("tracker.base_url = %q, want the value in the file to survive the promotion", tracker.BaseURL)
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

// A variable the file refers to is left out: that operator has done what the
// warning asks — and did not even have to rename anything — so going on warning
// about it would leave them nothing to fix and no way to silence it.
func TestWarnLegacyEnvPrefix_SilentForAVariableTheFileReferences(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	prev := environ
	t.Cleanup(func() { environ = prev })
	environ = func() []string { return []string{"K_NATS_URL=nats://nats:4222", "K_TRACKER_PORT=8080"} }
	t.Setenv("K_NATS_URL", "nats://nats:4222")

	writeConfig(t, "nats_url: env://K_NATS_URL\n")

	buf := captureSlog(t)
	if _, err := Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}

	logged := buf.String()
	if strings.Contains(logged, "K_NATS_URL") {
		t.Errorf("the variable the file names is migrated, not deprecated: %q", logged)
	}
	if !strings.Contains(logged, "K_TRACKER_PORT") {
		t.Errorf("expected the warning to still name the variable nothing reads, got %q", logged)
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

// The exported spelling of the admin token's variable and the derivation the
// promotion looks it up by have to agree, or the refusal an operator reads would
// name a variable Kannon does not read.
func TestLegacyEnvNameMatchesTheExportedAdminTokenSpelling(t *testing.T) {
	if got := legacyEnvName(APIAdminTokenKey); got != APIAdminTokenEnvVar {
		t.Errorf("legacyEnvName(%q) = %q, want %q", APIAdminTokenKey, got, APIAdminTokenEnvVar)
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
