package startup

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/x/config"
	"github.com/spf13/viper"
)

func TestRequireOwnMetricsPort(t *testing.T) {
	api := config.Services{API: config.Service{Enabled: true}}
	tracker := config.Services{Tracker: config.Service{Enabled: true}}

	tests := []struct {
		name     string
		services config.Services
		settings map[string]any
		wantKey  string
	}{
		{name: "defaults", services: config.AllServices()},
		{
			name:     "the Connect API port",
			services: api,
			settings: map[string]any{"metrics.port": 50051},
			wantKey:  "api.port",
		},
		{
			name:     "the REST API port",
			services: api,
			settings: map[string]any{"api.rest_port": 9090},
			wantKey:  "api.rest_port",
		},
		{
			name:     "the health port",
			services: api,
			settings: map[string]any{"api.health_port": 9090},
			wantKey:  "api.health_port",
		},
		{
			name:     "the tracker port",
			services: tracker,
			settings: map[string]any{"tracker.port": 9090},
			wantKey:  "tracker.port",
		},
		{
			name:     "the port of a component this process does not run",
			services: api,
			settings: map[string]any{"tracker.port": 9090},
		},
		{
			name:     "metrics disabled",
			services: api,
			settings: map[string]any{"metrics.enabled": false, "api.port": 9090},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			for k, v := range tc.settings {
				viper.Set(k, v)
			}

			_, err := loadMetrics(tc.services)
			if tc.wantKey == "" {
				if err != nil {
					t.Fatalf("expected the boot to be allowed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the boot to be refused")
			}
			for _, want := range []string{"metrics.port", tc.wantKey} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the refusal to name %q, got %q", want, err)
				}
			}
		})
	}
}
