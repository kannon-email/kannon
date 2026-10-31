package stats

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/x/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfig_AggregatedRetentionIsItsOwnKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("stats.retention", "48h")
	viper.Set("stats.aggregated_retention", "72h")

	var cfg Config
	config.LoadSection("stats", &cfg)
	cfg.setDefaults()

	assert.Equal(t, 48*time.Hour, cfg.Retention)
	assert.Equal(t, 72*time.Hour, cfg.AggregatedRetention)
}

func TestConfig_DefaultsToOneYear(t *testing.T) {
	var cfg Config
	cfg.setDefaults()

	assert.Equal(t, 8760*time.Hour, cfg.Retention)
	assert.Equal(t, 8760*time.Hour, cfg.AggregatedRetention)
}
