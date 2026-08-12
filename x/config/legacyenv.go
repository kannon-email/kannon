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
// could name the variables it wants. Removed in ADR 0012: it never reached more
// than a handful of keys, and which ones was invisible from the outside —
// K_API_PORT looked exactly as plausible as K_DATABASE_URL and did nothing.
const legacyEnvPrefix = "K"

// warnLegacyEnvPrefix names every K_ variable in the environment, once, at boot.
//
// The prefix is no longer read, which is why the warning outlived it: an
// installation upgrading without having migrated loses `K_DATABASE_URL` with no
// other sign than whatever fails next, and this line is what names the cause.
// A variable the file itself refers to is left out — that operator has done what
// the message asks, and their `env://K_…` reference goes on working under the
// name the deployment already gives it.
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
		"the %s_ environment prefix has been removed and is no longer read (%s); "+
			"name the variable in the config file instead, e.g. `database_url: env://%s_DATABASE_URL`, "+
			"which also works for the keys the prefix never reached",
		legacyEnvPrefix, strings.Join(found, ", "), legacyEnvPrefix))
}

// referencedEnvNames collects every environment variable the configuration refers
// to, at any depth, so that warnLegacyEnvPrefix can tell a variable an operator
// has migrated to naming from one that is setting nothing.
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
