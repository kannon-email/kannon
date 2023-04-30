package stats

import (
	"context"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"

	sq "github.com/ludusrusso/kannon/internal/stats_db"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go"
)

func Run(ctx context.Context) {
	dbURL := viper.GetString("stats_database_url")
	natsURL := viper.GetString("nats_url")

	logrus.Info("🚀 Starting stats")

	db, q, err := sq.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	_, js, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()

	handleStats(ctx, js, q)
}

func handleStats(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.*", "kannon-stats-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			data := &types.Stats{}
			err = proto.Unmarshal(msg.Data, data)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				stype := sq.GetStatsType(data)
				logrus.Printf("[%s] %s %s", StatsShow[stype], utils.ObfuscateEmail(data.Email), data.MessageId)
				err := q.InsertStat(ctx, sq.InsertStatParams{
					Email:     data.Email,
					MessageID: data.MessageId,
					Timestamp: data.Timestamp.AsTime(),
					Domain:    data.Domain,
					Type:      stype,
					Data:      data.Data,
				})
				if err != nil {
					logrus.Errorf("Cannot insert %v stat: %v", stype, err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
			}
		}
	}
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
