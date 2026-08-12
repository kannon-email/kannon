package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// The prefix is gone, and going means gone: a key the file says nothing about is
// unset, whatever a manifest left in the environment. Asserted on database_url
// because it is the one K_ variable that did work, so it is the one an upgrade
// could quietly lose.
func TestRead_NoLongerReadsTheLegacyEnvPrefix(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("K_DATABASE_URL", "postgres://legacy@db/kannon")

	prepareWithoutAConfigFile(t)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want nothing: the K_ prefix is no longer read", cfg.DatabaseURL)
	}
}

// Every K_ variable is named, not only the ones Kannon used to read: an
// installation that upgrades without having migrated loses them all at once, and
// this line is the only thing that names the cause.
func TestWarnLegacyEnvPrefix(t *testing.T) {
	prev := environ
	t.Cleanup(func() { environ = prev })
	environ = func() []string {
		return []string{"PATH=/usr/bin", "K_TRACKER_PORT=8080", "K_DATABASE_URL=postgres://db", "KANNON_ENABLE_API=true"}
	}

	buf := captureSlog(t)
	warnLegacyEnvPrefix()

	logged := buf.String()
	for _, want := range []string{"no longer read", "K_DATABASE_URL", "K_TRACKER_PORT", "env://"} {
		if !strings.Contains(logged, want) {
			t.Errorf("expected the warning to mention %q, got %q", want, logged)
		}
	}
	if strings.Contains(logged, "KANNON_ENABLE_API") {
		t.Errorf("a variable outside the prefix is not the removed one, got %q", logged)
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
		t.Errorf("the variable the file names is migrated, and still works: %q", logged)
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
