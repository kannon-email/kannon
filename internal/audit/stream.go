package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/nats-io/nats.go/jetstream"
)

// streamMaxAge is how long a Record sits on NATS. The stream is a buffer for a consumer that is down
// over a weekend, not a second archive: keeping it far below the table's retention avoids two
// archives with two expiries and two answers to the same question.
const streamMaxAge = 7 * 24 * time.Hour

// backlogCheckInterval is how often the producer looks for Records piling up with nothing consuming
// them. Minutes rather than seconds because it detects a configuration mistake, not an incident, and
// it has the whole of streamMaxAge to notice before anything is actually lost.
const backlogCheckInterval = 5 * time.Minute

// ConfigureStream ensures the audit stream exists. The one place this stream's configuration is
// written down, and called at startup by both the process that publishes Records and the one that
// consumes them: CreateOrUpdateStream is idempotent, and neither may depend on the other's boot order
// — the producer must not publish into a stream that does not exist, and the consumer must not
// require the API to have come up.
//
// It is deliberately absent from the embedded-NATS provisioning list, which creates streams with no
// MaxAge and would be a second definition of exactly what this function holds.
func ConfigureStream(ctx context.Context, sc utils.StreamCreator) error {
	return utils.ConfigureStream(ctx, sc, jetstream.StreamConfig{
		Name:        StreamName,
		Description: "Authorization decisions awaiting the audit writer",
		Replicas:    1,
		Subjects:    []string{StreamSubjects},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      streamMaxAge,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	})
}

// streamReader is the minimal seam over jetstream.JetStream needed to look at the audit stream's
// state. jetstream.JetStream satisfies it structurally.
type streamReader interface {
	Stream(ctx context.Context, name string) (jetstream.Stream, error)
}

// WatchBacklog warns while Audit Records are queueing with nothing consuming them — a deployment that
// enabled collection and forgot the worker, whose Records sit on the stream and expire. It returns
// only when ctx is done, so a caller runs it beside its real work and never on its critical path.
//
// A warning and not a failed health check: making the API unhealthy over an audit problem would have
// the pod killed, turning a gap in the register into an interruption of service.
func WatchBacklog(ctx context.Context, js streamReader) error {
	// The first check waits a full interval rather than running at once. On a cold boot the worker is
	// coming up alongside this process, and a check in the first milliseconds could see a backlog
	// left by the previous run with the new consumer not yet registered — a warning about damage that
	// is in the act of being repaired, which is exactly the kind that teaches an operator to ignore it.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backlogCheckInterval):
	}

	return runner.Run(ctx, func(ctx context.Context) error {
		warnOnBacklog(ctx, js)
		return nil
	}, runner.WaitLoop(backlogCheckInterval))
}

// warnOnBacklog writes the warning when, and only when, both halves of the damage are present:
// Records pending and no consumer. The conjunction is the point — pending alone is an ordinary
// moment in a healthy deployment, and no consumer alone is every boot before the worker connects.
//
// It swallows its own failures at debug. Not being able to read the stream is not itself a problem
// worth an operator's attention, and a check that shouted about NATS would be a second, worse
// monitor of something the container already reports on.
func warnOnBacklog(ctx context.Context, js streamReader) {
	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		slog.Debug("cannot read the audit stream to check for a backlog", "err", err)
		return
	}

	info, err := stream.Info(ctx)
	if err != nil {
		slog.Debug("cannot read the audit stream's state to check for a backlog", "err", err)
		return
	}

	// Msgs is what the stream is holding, not what is unacknowledged: under a limits policy an
	// acknowledged Record stays until it ages out, so this counts a little more than is strictly at
	// risk. The precise figure lives on a consumer, and the case worth warning about is the one where
	// there is no consumer to ask — so the reported number is named for what it actually is.
	if info.State.Msgs > 0 && info.State.Consumers == 0 {
		slog.Warn("Audit Records are queueing with nothing consuming them: they will expire unread. Run Kannon with --run-audit.",
			"stream", StreamName, "records_held", info.State.Msgs, "expires_after", streamMaxAge)
	}
}
