package stats

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/lib/pq"

	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go/jetstream"
)

type statsHandler struct {
	js jetstream.JetStream
	q  *sq.Queries
}

func Run(ctx context.Context, cnt *container.Container) error {
	q := cnt.Queries()
	js := cnt.NatsJetStream()

	statsHandler := statsHandler{
		js: js,
		q:  q,
	}
	return statsHandler.handleStats(ctx)
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
