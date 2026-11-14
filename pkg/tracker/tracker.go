package tracker

import (
	"context"
	"fmt"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
)

type Config struct {
	Port uint `mapstructure:"port"`
}

func (c *Config) setDefaults() {
	if c.Port == 0 {
		c.Port = 8080
	}
}

const configKey = "tracker"

// PortKey names the key the tracker's port is read from.
const PortKey = configKey + ".port"

// ListenPorts reads the port the tracker listens on, so the boot path can refuse another listener
// asked to share it.
func ListenPorts() ([]metrics.ListenPort, error) {
	var cfg Config
	if err := config.TryLoadSection(configKey, &cfg); err != nil {
		return nil, err
	}
	cfg.setDefaults()
	return []metrics.ListenPort{{Key: PortKey, Port: cfg.Port}}, nil
}

// New constructs the tracker runnable, loading its slice of configuration from
// viper under the "tracker" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection(configKey, &cfg)
	cfg.setDefaults()
	srv := NewServer(cnt, cfg)
	return container.Runnable{
		Name: "tracker",
		Run: func(ctx context.Context) error {
			// Opens and clicks are published on the stats stream.
			if err := stats.ConfigureStream(ctx, cnt.NatsJetStream()); err != nil {
				return fmt.Errorf("cannot configure stats stream: %w", err)
			}
			return srv.Run(ctx)
		},
	}
}
