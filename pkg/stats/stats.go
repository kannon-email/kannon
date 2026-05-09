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

// handleAggregatedStats consumes kannon.stats.* messages to update daily aggregated counters.
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

func (h *statsHandler) handleStatsMsg(ctx context.Context, msg jetstream.Msg) error {
	data := &types.Stats{}
	err := proto.Unmarshal(msg.Data(), data)
	if err != nil {
		return msg.Term()
	}

	stat := stats.NewStat(data.Email, data.MessageId, data.Domain, data.Timestamp.AsTime(), data.Data)

	slog.Info(fmt.Sprintf("[%s] %s %s", stats.DisplayName[stat.Type], utils.ObfuscateEmail(data.Email), data.MessageId))
	err = h.service.InsertStat(ctx, stat)
	if err != nil {
		slog.Error("cannot insert stat", "type", stat.Type, "err", err)
		return msg.Nak()
	}

	return msg.Ack()
}
