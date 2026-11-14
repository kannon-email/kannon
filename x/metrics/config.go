// Package metrics serves a process's Prometheus registry on a listener of its own, and holds the
// helpers Components use to register their collectors on it. See ADR 0018.
package metrics

import "github.com/kannon-email/kannon/x/config"

// PortKey names the key Port is read from, for the messages that have to point an operator at it.
const PortKey = sectionKey + ".port"

const (
	sectionKey  = "metrics"
	defaultPort = 9090
)

// Config is the top-level `metrics` section.
type Config struct {
	Enabled bool
	Port    uint
}

// LoadConfig reads the `metrics` section: enabled on port 9090 unless the file says otherwise.
func LoadConfig() (Config, error) {
	var raw struct {
		Enabled *bool `mapstructure:"enabled"`
		Port    uint  `mapstructure:"port"`
	}
	if err := config.TryLoadSection(sectionKey, &raw); err != nil {
		return Config{}, err
	}
	cfg := Config{Enabled: true, Port: raw.Port}
	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	return cfg, nil
}
