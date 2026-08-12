package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func assertEnabled(t *testing.T, got Services, want ...string) {
	t.Helper()
	if g := strings.Join(got.Enabled(), ","); g != strings.Join(want, ",") {
		t.Errorf("enabled services = [%s], want [%s]", g, strings.Join(want, ","))
	}
}

// Nothing runs unless the file says so, and an absent section is not an error
// here: the boot path is what refuses a process that would run nothing, with a
// message naming the section rather than a decode failure.
func TestLoadServices_NothingByDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	services, err := LoadServices()
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	assertEnabled(t, services)
}

// The shape the whole change exists for: one file, mounted by every Deployment of
// an installation, each of them enabling its own component through a variable.
func TestLoadServices_FromTheConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("KANNON_ENABLE_STATS", "true")

	writeConfig(t, `
services:
  api:
    enabled: true
  stats:
    enabled: env://KANNON_ENABLE_STATS:-false
  smtp:
    enabled: env://KANNON_ENABLE_SMTP:-false
`)

	if _, err := Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}

	services, err := LoadServices()
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	assertEnabled(t, services, "stats", "api")
}

// A misspelled service name is refused rather than ignored. Silently ignoring it
// would produce a process that runs nothing while its config file looks right,
// which is the one mistake this section must not make quietly.
func TestLoadServices_RefusesAnUnknownService(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("services.stat.enabled", true)

	_, err := LoadServices()
	if err == nil {
		t.Fatal("expected an error naming the unknown key")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("expected the error to name the unknown key, got %v", err)
	}
}

func TestLoadServices_ReportsAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("services.stats.enabled", "env://KANNON_ENABLE_STATS_NOBODY_SET")

	_, err := LoadServices()
	if err == nil {
		t.Fatal("expected an error naming the variable")
	}
	if !strings.Contains(err.Error(), "KANNON_ENABLE_STATS_NOBODY_SET") {
		t.Errorf("expected the error to name the variable, got %v", err)
	}
}

func TestAllServices(t *testing.T) {
	assertEnabled(t, AllServices(),
		"sender", "dispatcher", "validator", "stats", "tracker", "api", "smtp", "audit")
}
