package container

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// LoadConfig unmarshals the viper sub-tree at key into out. Panics on
// unmarshal failure with a message identifying the failing key — this is the
// only way runnables read their slice of configuration, and a malformed value
// here means the operator's YAML/env is wrong, which we want to surface
// immediately at boot rather than silently producing zero values.
func LoadConfig(key string, out any) {
	if err := viper.UnmarshalKey(key, out); err != nil {
		panic(fmt.Errorf("container: failed to load config %q: %w", key, err))
	}
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
		logrus.Warnf("config key %q is deprecated and will be removed in a future major version; use %q instead", a.oldKey, a.newKey)
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
			logrus.Warnf("config section %q is deprecated and will be removed in a future major version; use %q instead", oldSection, newSection)
			warnedSections[oldSection] = true
		}
		if !viper.IsSet(a.newKey) {
			viper.Set(a.newKey, viper.Get(a.oldKey))
		}
	}
}
