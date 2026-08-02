package stats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/kannon-email/kannon/x/container"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	Retention time.Duration `mapstructure:"retention"`
}

func (c *Config) setDefaults() {
	if c.Retention <= 0 {
		c.Retention = 8760 * time.Hour // 1 year
	}
}

type statsHandler struct {
	js        jetstream.JetStream
	service   *stats.Service
	q         *sq.Queries
	retention time.Duration
}

// New constructs the stats runnable, loading its slice of configuration from
// viper under the "stats" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	container.LoadConfig("stats", &cfg)
	cfg.setDefaults()
	return container.Runnable{
		Name: "stats",
		Run: func(ctx context.Context) error {
			return run(ctx, cnt, cfg)
		},
	}
}

func run(ctx context.Context, cnt *container.Container, cfg Config) error {
	q := cnt.Queries()
	js := cnt.NatsJetStream()

	db := cnt.DB()
	repo := sq.NewStatsRepository(db)
	aggregatedRepo := sq.NewAggregatedStatsRepository(db)
	service := stats.NewService(repo, stats.WithAggregatedStatsRepository(aggregatedRepo))

	h := statsHandler{
		js:        js,
		service:   service,
		q:         q,
		retention: cfg.Retention,
	}

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return h.handleStats(ctx)
	})

	eg.Go(func() error {
		return h.handleAggregatedStats(ctx)
	})

	eg.Go(func() error {
		return runner.Run(ctx, h.cleanupCycle, runner.WaitLoop(10*time.Minute))
	})

	return eg.Wait()
}

// handleStats consumes kannon.stats.* messages to persist individual stat records.
// This is intentionally a separate consumer from handleAggregatedStats so both
// can independently process the same messages for different purposes.
func (h *statsHandler) handleStats(ctx context.Context) error {
	con := utils.MustGetPullSubscriber(ctx, h.js, "kannon-stats", "kannon.stats.*", "kannon-stats-logs")
	c, err := con.Consume(func(msg jetstream.Msg) {
		if err := h.handleStatsMsg(ctx, msg); err != nil {
			slog.Error("Cannot handle stats msg", "err", err)
		}
	})
	if err != nil {
		return err
	}

	defer c.Drain()

	<-ctx.Done()
	return nil
}

func (h *statsHandler) cleanupCycle(ctx context.Context) error {
	deletedStats, err := h.service.Cleanup(ctx, h.retention)
	if err != nil {
		return err
	}

	deletedKeys, err := h.q.DeleteExpiredStatsKeys(ctx)
	if err != nil {
		return err
	}

	if deletedStats > 0 || deletedKeys > 0 {
		slog.Info(fmt.Sprintf("stats cleanup: deleted %d stats and %d expired keys", deletedStats, deletedKeys))
	}

	return nil
}

// handleAggregatedStats consumes kannon.stats.* messages to update hourly aggregated counters.
// Uses a separate consumer name ("kannon-aggregated-stats") from handleStats so both
// receive all messages independently.
func (h *statsHandler) handleAggregatedStats(ctx context.Context) error {
	con := utils.MustGetPullSubscriber(ctx, h.js, "kannon-stats", "kannon.stats.*", "kannon-aggregated-stats")
	c, err := con.Consume(func(msg jetstream.Msg) {
		if err := h.handleAggregatedStatsMsg(ctx, msg); err != nil {
			slog.Error("Cannot handle aggregated stats msg", "err", err)
		}
	})
	if err != nil {
		return err
	}

	defer c.Drain()

	<-ctx.Done()
	return nil
}

// handleAggregatedStatsMsg counts one stat event against its Domain's hourly
// counter. It reads only the Domain, the timestamp and the type, so it counts
// every Mode alike — including Anonymous, whose whole purpose is to reach this
// counter and no further.
//
// Splitting the subject so the two consumers no longer overlap was rejected: both
// subscribe to the single-token wildcard kannon.stats.*, which matches no longer
// subject, so the split would silently take the aggregate path down with it.
func (h *statsHandler) handleAggregatedStatsMsg(ctx context.Context, msg jetstream.Msg) error {
	data := &types.Stats{}
	if err := proto.Unmarshal(msg.Data(), data); err != nil {
		return msg.Term()
	}

	statType := stats.DetermineTypeFromStats(data)
	if err := h.service.IncrementAggregatedStat(ctx, data.Domain, data.Timestamp.AsTime(), statType); err != nil {
		slog.Error("cannot increment aggregated stat", "err", err)
		return msg.Nak()
	}

	return msg.Ack()
}

// handleStatsMsg writes the per-recipient row for one stat event.
//
// Under Anonymous it writes none. That Mode is counted in aggregate only —
// nothing is retained that could isolate one Recipient from another (CONTEXT.md) —
// and the identity such an event carries names nobody by construction: the
// Anonymous sentinel of its Domain, or nothing at all on a token minted before the
// identity claim was always email-shaped. handleAggregatedStats is an independent
// subscription on the same subject and counts the event regardless, so the Domain
// keeps its open and click rates.
//
// Every other Mode names somebody, and so does every event that is not an
// engagement. Pseudonymous names a pseudonym rather than a Recipient —
// `<rand>@track.<domain>`, drawn per Delivery and linkable to nothing outside its
// Batch (ADR 0006) — and it takes this same path deliberately: the identity claim
// is email-shaped whichever Mode produced it, so the row, the schema and counting
// distinct addresses all keep working unchanged, and the Mode on the event is what
// says which kind of address was written.
//
// An event that names nobody yet is not Anonymous is a bug upstream, and in a
// compliance path a loud failure beats a quietly lost row: it is logged as an
// error rather than dropped in silence. Naming nobody is a question about the
// address and not merely about emptiness — the sentinel is an ordinary address to
// the schema, so an event carrying it under any other Mode would otherwise be
// recorded as though somebody were called `anonymous@track.<domain>`. It is Termed
// and not Nak'd, because the fault is in the message itself — redelivering it could
// only reproduce the hot loop #396 fixed.
func (h *statsHandler) handleStatsMsg(ctx context.Context, msg jetstream.Msg) error {
	data := &types.Stats{}
	if err := proto.Unmarshal(msg.Data(), data); err != nil {
		return msg.Term()
	}

	stat := stats.NewStat(data.Email, data.MessageId, data.Domain, data.Timestamp.AsTime(), data.Data)
	mode := trackingpb.ToMode(data.TrackingMode)

	if mode == tracking.ModeAnonymous {
		slog.Debug("anonymous event: counted in aggregate, no per-recipient row",
			"type", stat.Type, "batch", data.MessageId, "domain", data.Domain)
		return msg.Ack()
	}

	if namesNobody(data.Email, data.Domain) {
		slog.Error("stat event carries no recipient identity and is not anonymous",
			"type", stat.Type, "batch", data.MessageId, "domain", data.Domain, "tracking_mode", mode)
		return msg.Term()
	}

	slog.Info(fmt.Sprintf("[%s] %s %s", stats.DisplayName[stat.Type], utils.ObfuscateEmail(data.Email), data.MessageId))
	if err := h.service.InsertStat(ctx, stat); err != nil {
		slog.Error("cannot insert stat", "type", stat.Type, "err", err)
		return msg.Nak()
	}

	return msg.Ack()
}

// namesNobody reports whether a stat event's identity claim stands for no one.
// Two shapes say that: the Anonymous sentinel of the event's own Domain, which is
// what a token minted under Anonymous carries today (ADR 0006), and an empty
// claim, which is what a token minted before the claim was always email-shaped
// carries — those stay in circulation for one token lifetime and cannot be
// dropped from the check until they expire.
//
// A pseudonym is *not* one of them: it names nobody outside its Batch, but within
// one it is exactly what tells two Recipients apart, and that is what the row
// records.
func namesNobody(email, domain string) bool {
	return email == "" || email == tracking.AnonymousIdentity(domain)
}
