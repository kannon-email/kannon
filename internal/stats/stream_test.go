package stats_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stream created before the age limit existed — by embedded provisioning or by hand — is brought
// under it, and configuring it again from a second process changes nothing.
func TestConfigureStreamBoundsAnExistingStream(t *testing.T) {
	js := tests.NatsJetStream(t)
	_, err := js.CreateStream(t.Context(), jetstream.StreamConfig{Name: stats.StreamName, Subjects: []string{stats.StreamSubjects}})
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, stats.ConfigureStream(t.Context(), js))
	}

	stream, err := js.Stream(t.Context(), stats.StreamName)
	require.NoError(t, err)
	cfg := stream.CachedInfo().Config
	assert.Equal(t, stats.StreamMaxAge, cfg.MaxAge)
	assert.Equal(t, []string{"kannon.stats.*"}, cfg.Subjects)
	assert.Equal(t, jetstream.LimitsPolicy, cfg.Retention)
	assert.Equal(t, jetstream.FileStorage, cfg.Storage)
	assert.Equal(t, jetstream.DiscardOld, cfg.Discard)
}
