package container

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestLoadConfig_HappyPath(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("foo.name", "alice")
	viper.Set("foo.count", 7)

	var out struct {
		Name  string `mapstructure:"name"`
		Count int    `mapstructure:"count"`
	}
	LoadConfig("foo", &out)

	if out.Name != "alice" || out.Count != 7 {
		t.Errorf("unexpected unmarshal result: %+v", out)
	}
}

func TestLoadConfig_PanicsOnTypeMismatch(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("foo.count", "not-a-number")

	var out struct {
		Count int `mapstructure:"count"`
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on type mismatch")
		}
		msg, ok := r.(error)
		if !ok || !strings.Contains(msg.Error(), "foo") {
			t.Errorf("expected panic mentioning key, got %v", r)
		}
	}()
	LoadConfig("foo", &out)
}

// There is no default, and an unset key must answer with nothing rather than with something a
// caller could authenticate against. Asserted with no key set at all, because a reader running
// without the cmd layer's config setup must still get the documented answer.
func TestAPIAdminToken_EmptyWhenUnset(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	if got := APIAdminToken(); got != "" {
		t.Errorf("expected no admin token when the key is unset, got %q", got)
	}
}

func TestAPIAdminToken_FromTheConfigFileKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(APIAdminTokenKey, "s3cr3t")

	if got := APIAdminToken(); got != "s3cr3t" {
		t.Errorf("APIAdminToken() = %q, want %q", got, "s3cr3t")
	}
}

// The environment spelling is the half of the contract a Kubernetes manifest uses, and it is the
// one viper would otherwise lose: a nested key reached through UnmarshalKey never consults the
// environment. Read with nothing else configured, the way a container running only on env vars is.
func TestAPIAdminToken_FromTheEnvironment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv(APIAdminTokenEnvVar, "s3cr3t-from-env")

	if got := APIAdminToken(); got != "s3cr3t-from-env" {
		t.Errorf("APIAdminToken() = %q, want the value of %s", got, APIAdminTokenEnvVar)
	}
}

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
