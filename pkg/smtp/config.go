package smtp

import "time"

type Config struct {
	Address         string
	Domain          string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxPayloadBytes uint
	MaxRecipients   uint
}

// GetAddress returns the SMTP server address with default fallback
func (c Config) GetAddress() string {
	if c.Address == "" {
		return ":25"
	}
	return c.Address
}

// GetDomain returns the SMTP server domain with default fallback
func (c Config) GetDomain() string {
	if c.Domain == "" {
		return "localhost"
	}
	return c.Domain
}

// GetReadTimeout returns the SMTP read timeout with default fallback
func (c Config) GetReadTimeout() time.Duration {
	if c.ReadTimeout <= 0 {
		return 10 * time.Second
	}
	return c.ReadTimeout
}

// GetWriteTimeout returns the SMTP write timeout with default fallback
func (c Config) GetWriteTimeout() time.Duration {
	if c.WriteTimeout <= 0 {
		return 10 * time.Second
	}
	return c.WriteTimeout
}

// GetMaxPayload returns the SMTP max payload with default fallback
func (c Config) GetMaxPayload() uint {
	if c.MaxPayloadBytes == 0 {
		return 1024 * 1024
	}
	return c.MaxPayloadBytes
}

// GetMaxRecipients returns the SMTP max recipients with default fallback
func (c Config) GetMaxRecipients() uint {
	if c.MaxRecipients == 0 {
		return 50
	}
	return c.MaxRecipients
}
