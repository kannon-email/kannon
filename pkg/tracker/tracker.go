package tracker

import (
	"context"
	"net"

	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
)

type Config struct {
	Port uint
}

const (
	configKey   = "tracker"
	defaultPort = 8080
)

// PortKey names the key the tracker's port is read from.
const PortKey = configKey + ".port"

// LoadConfig reads the `tracker` section. An unset port is 8080; port 0 lets the system pick one.
func LoadConfig() (Config, error) {
	var raw struct {
		Port *uint `mapstructure:"port"`
	}
	if err := config.TryLoadSection(configKey, &raw); err != nil {
		return Config{}, err
	}
	return Config{Port: config.Or(raw.Port, defaultPort)}, nil
}

// ListenPorts are the ports the tracker listens on, so the boot path can refuse another listener
// asked to share one.
func (c Config) ListenPorts() []metrics.ListenPort {
	return []metrics.ListenPort{{Key: PortKey, Port: c.Port}}
}

// Listen binds the tracker's listener on cfg's port.
func Listen(cfg Config) (net.Listener, error) {
	return container.Listen(cfg.Port)
}

// New constructs the tracker runnable, serving on l.
func New(cnt *container.Container, l net.Listener) container.Runnable {
	srv := NewServer(cnt)
	return container.Runnable{
		Name: "tracker",
		Run: func(ctx context.Context, ready func()) error {
			ready()
			return srv.Run(ctx, l)
		},
	}
}
