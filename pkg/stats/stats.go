package stats

import (
	"context"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/sync/errgroup"

	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	Retention time.Duration
}

type statsHandler struct {
	js        jetstream.JetStream
	service   *stats.Service
	q         *sq.Queries
	retention time.Duration
}

func Run(ctx context.Context, cnt *container.Container, cfg Config) error {
	q := cnt.Queries()
	js := cnt.NatsJetStream()

	repo := sq.NewStatsRepository(q)
	service := stats.NewService(repo)

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
		return runner.Run(ctx, h.cleanupCycle, runner.WaitLoop(10*time.Minute))
	})

	return eg.Wait()
}

func (h *statsHandler) handleStats(ctx context.Context) error {
	con := utils.MustGetPullSubscriber(ctx, h.js, "kannon-stats", "kannon.stats.*", "kannon-stats-logs")
	c, err := con.Consume(func(msg jetstream.Msg) {
		if err := h.handleStatsMsg(ctx, msg); err != nil {
			logrus.Errorf("Cannot handle stats msg: %v", err)
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
		logrus.Infof("stats cleanup: deleted %d stats and %d expired keys", deletedStats, deletedKeys)
	}

	return nil
}

func (h *statsHandler) handleStatsMsg(ctx context.Context, msg jetstream.Msg) error {
	data := &types.Stats{}
	err := proto.Unmarshal(msg.Data(), data)
	if err != nil {
		return msg.Term()
	}

	stat := stats.NewStat(data.Email, data.MessageId, data.Domain, data.Timestamp.AsTime(), data.Data)

	logrus.Printf("[%s] %s %s", stats.DisplayName[stat.Type], utils.ObfuscateEmail(data.Email), data.MessageId)
	err = h.service.InsertStat(ctx, stat)
	if err != nil {
		logrus.Errorf("cannot insert %v stat: %v", stat.Type, err)
		return msg.Nak()
	}

	return msg.Ack()
}
