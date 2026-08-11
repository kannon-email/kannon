package config

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/kannon-email/kannon/x/config/envref"
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
var legacyEnvKeys = []string{"database_url", "nats_url", "use_embedded_nats", "debug", APIAdminTokenKey}

// prepareLegacyEnv teaches viper the deprecated variable naming. It no longer
// turns AutomaticEnv on, nor binds the keys above: both put the environment
// *above* the configuration file, and promoteLegacyEnv puts it below. What is
// left is what the one remaining binding — the `bump` section's port — needs to
// derive its variable name from its key.
func prepareLegacyEnv() {
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetEnvPrefix(legacyEnvPrefix)
}

// legacyEnvName is the deprecated environment spelling of a config key: the
// prefix, then the key upper-cased with its separator replaced. The same
// derivation viper's own BindEnv performs, written out because the promotion
// below deliberately does not go through viper's environment layer.
func legacyEnvName(key string) string {
	return legacyEnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// promoteLegacyEnv copies the deprecated K_ variables onto their keys — but only
// the keys the configuration file says nothing about.
//
// viper.BindEnv is what this used to be, and it inverted the contract this whole
// change exists to establish: viper ranks the environment above the file, so a
// K_ variable left behind in a manifest beat the `env://` reference the file had
// just been migrated to name. An operator who moved api.admin_token into the file
// and rotated the Secret it names would have gone on authenticating callers with
// the stale credential, told only that some prefix was deprecated. The file is
// the contract; the deprecated variable is a fallback for what the file does not
// say, and nothing more.
func promoteLegacyEnv() {
	for _, key := range legacyEnvKeys {
		if viper.InConfig(key) {
			continue
		}
		if value, ok := os.LookupEnv(legacyEnvName(key)); ok {
			setNested(key, value)
		}
	}
}

// setNested writes value at a dotted key without hiding the keys beside it.
//
// viper.Set would: a nested Set puts a partial map in the override layer, and Get
// answers from that layer alone rather than merging it, so
// viper.Set("api.admin_token", …) costs the operator their api.port. That is the
// trap ADR 0011 lists among its rejected alternatives, and the one this file used
// to rely on being harmless because `tracker` happened to have exactly one key.
// MergeConfigMap deep-merges into the config layer instead — which is also the
// right precedence for a value standing in for something the file could have said
// itself.
func setNested(key string, value any) {
	parts := strings.Split(key, ".")
	nested := map[string]any{parts[len(parts)-1]: value}
	for i := len(parts) - 2; i >= 0; i-- {
		nested = map[string]any{parts[i]: nested}
	}
	//nolint:errcheck // MergeConfigMap returns nil on every path.
	viper.MergeConfigMap(nested)
}

// warnLegacyEnvPrefix names every K_ variable in the environment, once, at boot.
//
// Every one of them, not only the keys Kannon reads: a deployment carrying
// K_TRACKER_PORT has been setting nothing for as long as it has existed, and the
// operator has no way to tell from the outside. The replacement does not even ask
// them to rename anything — the file can go on referring to the variable that is
// already there — which is exactly why a variable the file does refer to is left
// out of the warning: that operator has done what this message asks, and one that
// went on firing afterwards could never be cleared.
func warnLegacyEnvPrefix() {
	referenced := referencedEnvNames()

	var found []string
	for _, entry := range environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, legacyEnvPrefix+"_") && !referenced[name] {
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

// referencedEnvNames collects every environment variable the configuration refers
// to, at any depth, so that warnLegacyEnvPrefix can tell a variable an operator
// has migrated to naming from one that is still setting nothing.
func referencedEnvNames() map[string]bool {
	names := map[string]bool{}

	var walk func(value any)
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			if name, ok := envref.Name(v); ok {
				names[name] = true
			}
		case map[string]any:
			for _, e := range v {
				walk(e)
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(viper.AllSettings())

	return names
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
			// setNested and not viper.Set: the canonical key is nested, and a
			// nested Set would hide every sibling of it from the section read.
			setNested(a.newKey, viper.Get(a.oldKey))
		}
	}
}

// DeprecatedRunFlagMessage is what cobra prints when a --run-* flag is used. In
// the flag package's own phrasing, which appends it after "Flag --run-x has been
// deprecated,".
func DeprecatedRunFlagMessage(service string) string {
	return fmt.Sprintf("set `%s.%s.enabled` in the config file instead", servicesKey, service)
}
