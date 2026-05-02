package smtp

import "time"

type Config struct {
	Address         string        `mapstructure:"address"`
	Domain          string        `mapstructure:"domain"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	MaxPayloadBytes uint          `mapstructure:"max_payload"`
	MaxRecipients   uint          `mapstructure:"max_recipients"`
}

func (c *Config) setDefaults() {
	if c.Address == "" {
		c.Address = ":25"
	}
	if c.Domain == "" {
		c.Domain = "localhost"
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 10 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.MaxPayloadBytes == 0 {
		c.MaxPayloadBytes = 1024 * 1024
	}
	if c.MaxRecipients == 0 {
		c.MaxRecipients = 50
	}
}
