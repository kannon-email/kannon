// Package metrics serves a process's Prometheus registry on a listener of its own, and holds the
// helpers Components use to register their collectors on it. See ADR 0018.
package metrics

import (
	"fmt"

	"github.com/kannon-email/kannon/x/config"
)

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

// LoadConfig reads the `metrics` section: enabled on port 9090 unless the file says otherwise. Port 0
// lets the system pick one.
func LoadConfig() (Config, error) {
	var raw struct {
		Enabled *bool `mapstructure:"enabled"`
		Port    *uint `mapstructure:"port"`
	}
	if err := config.TryLoadSection(sectionKey, &raw); err != nil {
		return Config{}, err
	}
	cfg := Config{Enabled: true, Port: defaultPort}
	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	if raw.Port != nil {
		cfg.Port = *raw.Port
	}
	return cfg, nil
}

// ListenPort is a port another listener of the process opens, with the config key that sets it.
type ListenPort struct {
	Key  string
	Port uint
}

// RequireOwnPort refuses a metrics listener asked to share a port with another listener the process
// runs, naming the config key that sets it. Port 0 collides with nothing: each listener asking for it
// is given a free port of its own.
func (c Config) RequireOwnPort(others []ListenPort) error {
	if !c.Enabled || c.Port == 0 {
		return nil
	}
	for _, other := range others {
		if other.Port == c.Port {
			return fmt.Errorf("%s and %s are both %d: the metrics listener needs a port of its own", PortKey, other.Key, c.Port)
		}
	}
	return nil
}
