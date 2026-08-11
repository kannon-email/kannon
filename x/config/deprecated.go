package config

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/spf13/viper"
)

// environ is os.Environ, replaced by the test that asserts the absence of a
// warning: a K_ variable left over in the developer's own shell would otherwise
// decide the result.
var environ = os.Environ

// legacyEnvPrefix is the prefix viper matched settings against before a file
// could name the variables it wants. Deprecated: it never reached more than a
// handful of keys, and which ones was invisible from the outside — K_API_PORT
// looked exactly as plausible as K_DATABASE_URL and did nothing.
const legacyEnvPrefix = "K"

// legacyEnvKeys are the keys a K_-prefixed variable has actually been able to
// set, and which therefore have to keep working.
//
// They are bound explicitly, and not left to AutomaticEnv, because an unbound
// variable is invisible to viper.Unmarshal: AllKeys lists the keys viper knows
// about, and a variable nobody bound is not one of them. AutomaticEnv would still
// answer a Get for these — it is the reason they worked at all — but the
// configuration is read through structs now.
var legacyEnvKeys = []string{"database_url", "nats_url", "use_embedded_nats", "debug"}

func prepareLegacyEnv() {
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetEnvPrefix(legacyEnvPrefix)
	viper.AutomaticEnv()

	for _, key := range legacyEnvKeys {
		//nolint:errcheck
		viper.BindEnv(key)
	}
}

// warnLegacyEnvPrefix names every K_ variable in the environment, once, at boot.
//
// Every one of them, not only the keys Kannon reads: a deployment carrying
// K_TRACKER_PORT has been setting nothing for as long as it has existed, and the
// operator has no way to tell from the outside. The replacement does not even ask
// them to rename anything — the file can go on referring to the variable that is
// already there.
func warnLegacyEnvPrefix() {
	var found []string
	for _, entry := range environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, legacyEnvPrefix+"_") {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return
	}
	slices.Sort(found)

	slog.Warn(fmt.Sprintf(
		"the %s_ environment prefix is deprecated and will be removed in a future major version (%s); "+
			"name the variable in the config file instead, e.g. `database_url: env://%s_DATABASE_URL`, "+
			"which also works for the keys the prefix never reached",
		legacyEnvPrefix, strings.Join(found, ", "), legacyEnvPrefix))
}

// ApplyDeprecatedAliases promotes deprecated config keys onto their canonical
// names and logs a one-line deprecation warning at startup. Each entry is a
// public API surface we still owe users.
func ApplyDeprecatedAliases() {
	boolAliases := []struct {
		oldKey string
		newKey string
	}{
		{oldKey: "run-verifier", newKey: "run-validator"},
		{oldKey: "run-bounce", newKey: "run-tracker"},
	}

	for _, a := range boolAliases {
		if !viper.GetBool(a.oldKey) {
			continue
		}
		slog.Warn(fmt.Sprintf("config key %q is deprecated and will be removed in a future major version; use %q instead", a.oldKey, a.newKey))
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
			slog.Warn(fmt.Sprintf("config section %q is deprecated and will be removed in a future major version; use %q instead", oldSection, newSection))
			warnedSections[oldSection] = true
		}
		if !viper.IsSet(a.newKey) {
			viper.Set(a.newKey, viper.Get(a.oldKey))
		}
	}
}

// DeprecatedRunFlagMessage is what cobra prints when a --run-* flag is used. In
// the flag package's own phrasing, which appends it after "Flag --run-x has been
// deprecated,".
func DeprecatedRunFlagMessage(service string) string {
	return fmt.Sprintf("set `%s.%s.enabled` in the config file instead", servicesKey, service)
}
