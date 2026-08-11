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

// Nothing runs unless something says so, and an absent section is not an error:
// a deployment migrating one component at a time still has flags for the rest.
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

// The deprecated flag and the section are OR-ed, so a pod still passing --run-api
// keeps working next to one that has moved to the file.
func TestLoadServices_DeprecatedFlagsStillEnable(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("services.stats.enabled", true)
	viper.Set("run-api", true)

	services, err := LoadServices()
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	assertEnabled(t, services, "stats", "api")
}

// A flag saying nothing does not turn off what the file turned on: false is the
// flag's default, not an instruction.
func TestLoadServices_AFalseFlagDoesNotDisable(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("services.stats.enabled", true)
	viper.Set("run-stats", false)

	services, err := LoadServices()
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	assertEnabled(t, services, "stats")
}

// Regression, from when these were flags: --run-bounce / --run-verifier must be
// promoted onto their canonical names before the services section is read, or the
// alias fires too late, nothing is registered, and the process exits as if it had
// been asked to run nothing.
func TestLoadServices_DeprecatedAliasesArePromotedFirst(t *testing.T) {
	for _, tc := range []struct {
		alias string
		want  string
	}{
		{alias: "run-bounce", want: "tracker"},
		{alias: "run-verifier", want: "validator"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)

			viper.Set(tc.alias, true)

			if _, err := Read(); err != nil {
				t.Fatalf("Read: %v", err)
			}
			services, err := LoadServices()
			if err != nil {
				t.Fatalf("LoadServices: %v", err)
			}
			assertEnabled(t, services, tc.want)
		})
	}
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

// The name in the file, the name of the deprecated flag and the name in a log
// line are one string, and this is what says so.
func TestDeprecatedRunKeyMatchesEveryService(t *testing.T) {
	var s Services
	for _, svc := range s.each() {
		if got, want := deprecatedRunKey(svc.name), "run-"+svc.name; got != want {
			t.Errorf("deprecatedRunKey(%q) = %q, want %q", svc.name, got, want)
		}
	}
}
