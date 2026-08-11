package audit

import (
	"time"

	"github.com/kannon-email/kannon/x/config"
)

// configKey is the section an operator writes this under. Named once, because two processes read it:
// a second spelling would leave one of them silently reading defaults.
const configKey = "audit"

// DefaultRetention is how long an Audit Record is kept when an operator names no figure: one month.
// A default so that the feature is useful without somebody having to decide a number before they
// know what they need, and a month because that is the shortest span over which the questions this
// table exists to answer — what did this credential do, who touched that Domain — are still asked.
const DefaultRetention = 720 * time.Hour

// Config is the audit slice of an operator's configuration, read from viper under "audit" by both
// the process that produces Records and the one that writes them. One type, so the two cannot come
// to disagree about whether the feature is on or about how long a Record lives.
type Config struct {
	// Enabled governs whether anything is collected at all. False by default: a system whose
	// posture is to retain no personal data must not begin retaining some because a deployment
	// was upgraded, and with this set the API process starts talking to NATS, which it does not
	// do otherwise. Not running the worker is not a way to switch this off — the Records would
	// still be published and still sit on the stream — so the switch is here, at the producer.
	Enabled bool `mapstructure:"enabled"`

	// Retention is how long a Record is kept. A configured value and not a constant, because an
	// Audit Record holds an Attribution, an Attribution is personal data, and the retention of
	// personal data is the operator's legal obligation rather than Kannon's preference — the
	// same reason stats.retention is a key over rows carrying addresses and IPs.
	Retention time.Duration `mapstructure:"retention"`
}

// LoadConfig reads the audit section, defaults filled in. Read here rather than by each runnable that
// needs it — the producer and the consumer both do — so that the section name and the default cannot
// come to be spelled two ways, for the same reason ConfigureStream holds the stream's configuration
// once. It panics on a malformed section, as every other section read on the boot path does, since
// that is the operator's file being wrong and not a runtime condition.
func LoadConfig() Config {
	cfg, err := TryLoadConfig()
	if err != nil {
		panic(err)
	}
	return cfg
}

// TryLoadConfig is LoadConfig for the reader that must not fail on it: the API resolves this section
// from inside its own runnable, where the panic would land in a goroutine nobody recovers and take
// the whole process down — over a feature whose entire promise is that an audit trail Kannon cannot
// write is not an outage for somebody's customers.
func TryLoadConfig() (Config, error) {
	var cfg Config
	if err := config.TryLoadSection(configKey, &cfg); err != nil {
		return Config{}, err
	}
	cfg.setDefaults()
	return cfg, nil
}

// setDefaults fills in what an operator left unset.
func (c *Config) setDefaults() {
	if c.Retention <= 0 {
		c.Retention = DefaultRetention
	}
}
