package tracker

import (
	"context"
	"fmt"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
)

type Config struct {
	Port uint `mapstructure:"port"`
}

func (c *Config) setDefaults() {
	if c.Port == 0 {
		c.Port = 8080
	}
}

// ListenPorts reads the port the tracker listens on, keyed by the config key that sets it.
func ListenPorts() (map[string]uint, error) {
	var cfg Config
	if err := config.TryLoadSection("tracker", &cfg); err != nil {
		return nil, err
	}
	cfg.setDefaults()
	return map[string]uint{"tracker.port": cfg.Port}, nil
}

// New constructs the tracker runnable, loading its slice of configuration from
// viper under the "tracker" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection("tracker", &cfg)
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
