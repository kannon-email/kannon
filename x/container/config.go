package container

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

// LoadConfig unmarshals the viper sub-tree at key into out, panicking on failure with a message
// naming the key. This is the only way runnables read configuration, and a malformed value means
// the operator's YAML/env is wrong, which must surface at boot rather than as zero values.
func LoadConfig(key string, out any) {
	if err := viper.UnmarshalKey(key, out); err != nil {
		panic(fmt.Errorf("container: failed to load config %q: %w", key, err))
	}
}

// APIAdminTokenKey holds the credential that authenticates the Admin API and both Stats API
// versions (ADR 0009). Exported because the operator has to be told which key to set when it is
// missing, and a message naming a key the code does not read is worse than no message.
const APIAdminTokenKey = "api.admin_token"

// APIAdminTokenEnvVar is the same key's environment spelling: the prefix and the dot replacement
// prepareConfig installs, applied by hand. Written out rather than derived because it is the half
// of the contract an operator meets in a Kubernetes manifest, and a test pins the two together.
const APIAdminTokenEnvVar = "K_API_ADMIN_TOKEN"

// APIAdminToken returns the configured admin credential, empty when none is set — the caller
// decides what that means, which for the API runnable is a refusal to boot.
//
// Bound and read key by key rather than through the "api" section's struct: viper's UnmarshalKey
// does not consult the environment for a nested key, so a token supplied only as
// K_API_ADMIN_TOKEN would unmarshal to no token at all. That is the one way this key must not
// fail, since it would silently leave the surfaces it protects unreachable.
func APIAdminToken() string {
	//nolint:errcheck
	viper.BindEnv(APIAdminTokenKey, APIAdminTokenEnvVar)
	return viper.GetString(APIAdminTokenKey)
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
