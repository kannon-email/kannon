package metrics_test

import (
	"testing"

	"github.com/kannon-email/kannon/x/metrics"
	"github.com/spf13/viper"
)

func TestLoadConfigDefaultsToEnabledOn9090(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := metrics.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Enabled || cfg.Port != 9090 {
		t.Fatalf("got %+v, want enabled on 9090", cfg)
	}
}

func TestLoadConfigReadsReferences(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("KANNON_METRICS_ENABLED", "false")
	viper.Set("metrics", map[string]any{
		"enabled": "env://KANNON_METRICS_ENABLED",
		"port":    "env://KANNON_METRICS_PORT:-9191",
	})

	cfg, err := metrics.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Enabled || cfg.Port != 9191 {
		t.Fatalf("got %+v, want disabled on 9191", cfg)
	}
}

func TestLoadConfigRefusesAnUnsetReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("metrics", map[string]any{"port": "env://KANNON_METRICS_PORT_NOBODY_SET"})

	if _, err := metrics.LoadConfig(); err == nil {
		t.Fatal("expected an unset reference to be refused")
	}
}
