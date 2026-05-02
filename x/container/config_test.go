package container

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

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

func TestApplyDeprecatedAliases_RunVerifier(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	var buf bytes.Buffer
	prev := logrus.StandardLogger().Out
	logrus.SetOutput(&buf)
	t.Cleanup(func() { logrus.SetOutput(prev) })

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

	var buf bytes.Buffer
	prev := logrus.StandardLogger().Out
	logrus.SetOutput(&buf)
	t.Cleanup(func() { logrus.SetOutput(prev) })

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
