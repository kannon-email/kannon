package tracker

import (
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

// New constructs the tracker runnable, loading its slice of configuration from
// viper under the "tracker" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	container.LoadConfig("tracker", &cfg)
	cfg.setDefaults()
	srv := NewServer(cnt, cfg)
	return container.Runnable{
		Name: "tracker",
		Run:  srv.Run,
	}
}
