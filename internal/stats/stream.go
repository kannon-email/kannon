package stats

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// StreamName is the JetStream stream every stat event is stored on.
	StreamName = "kannon-stats"
	// StreamSubjects matches every stat subject publisher.PublishStat derives.
	StreamSubjects = "kannon.stats.*"
)

// StreamMaxAge is how long a stat event stays on the stream: a buffer for consumers that are down
// over a weekend, not an archive — the database is. It is also the longest a stored event can still
// be redelivered.
const StreamMaxAge = 7 * 24 * time.Hour

// ConfigureStream ensures the stats stream exists. The one place this stream's configuration is
// written down, called at startup by every process that publishes stat events or consumes them:
// CreateOrUpdateStream is idempotent, and none of them may depend on another's boot order — a stat
// is published with a plain NATS publish, so one sent before the stream exists is silently lost.
//
// It is deliberately absent from the embedded-NATS provisioning list, which creates streams with no
// MaxAge and would be a second definition of exactly what this function holds.
func ConfigureStream(ctx context.Context, sc utils.StreamCreator) error {
	return utils.ConfigureStream(ctx, sc, jetstream.StreamConfig{
		Name:        StreamName,
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{StreamSubjects},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      StreamMaxAge,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	})
}
