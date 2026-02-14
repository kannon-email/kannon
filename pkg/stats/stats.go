package stats

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/lib/pq"
	"golang.org/x/sync/errgroup"

	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
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
	q         *sq.Queries
	retention time.Duration
}

func Run(ctx context.Context, cnt *container.Container, cfg Config) error {
	q := cnt.Queries()
	js := cnt.NatsJetStream()

	h := statsHandler{
		js:        js,
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
	before := pgtype.Timestamp{
		Time:  time.Now().Add(-h.retention),
		Valid: true,
	}

	deletedStats, err := h.q.DeleteStatsOlderThan(ctx, before)
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

	stype := sq.GetStatsType(data)

	logrus.Printf("[%s] %s %s", StatsShow[stype], utils.ObfuscateEmail(data.Email), data.MessageId)
	err = h.insertStat(ctx, data)
	if err != nil {
		logrus.Errorf("cannot insert %v stat: %v", stype, err)
		return msg.Nak()
	}

	return msg.Ack()
}

func (h *statsHandler) insertStat(ctx context.Context, data *types.Stats) error {
	stype := sq.GetStatsType(data)
	return h.q.InsertStat(ctx, sq.InsertStatParams{
		Email:     data.Email,
		MessageID: data.MessageId,
		Timestamp: pgtype.Timestamp{
			Time:  data.Timestamp.AsTime(),
			Valid: true,
		},
		Domain: data.Domain,
		Type:   stype,
		Data:   data.Data,
	})
}

var StatsShow = map[sq.StatsType]string{
	sq.StatsTypeAccepted:  "✅ Accepted",
	sq.StatsTypeRejected:  "🛑 Rejected",
	sq.StatsTypeBounce:    "💥 Bounced",
	sq.StatsTypeClicked:   "🔗 Clicked",
	sq.StatsTypeDelivered: "🚀 Delivered",
	sq.StatsTypeError:     "😡 Send Error",
	sq.StatsTypeOpened:    "👀 Opened",
	sq.StatsTypeUnknown:   "😅 Unknown",
}
