package stats

import (
	"context"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"

	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/ludusrusso/kannon/generated/pb/stats/types"
	sq "github.com/ludusrusso/kannon/internal/stats_db"
	"github.com/ludusrusso/kannon/internal/utils"
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

	var wg sync.WaitGroup
	wg.Add(6)

	go func() {
		handleErrors(ctx, js, q)
		wg.Done()
	}()
	go func() {
		handleDelivereds(ctx, js, q)
		wg.Done()
	}()
	go func() {
		handleSends(ctx, js, q)
		wg.Done()
	}()
	go func() {
		handleOpens(ctx, js, q)
		wg.Done()
	}()
	go func() {
		handleClicks(ctx, js, q)
		wg.Done()
	}()
	go func() {
		handleSoftBounce(ctx, js, q)
		wg.Done()
	}()
	wg.Wait()
}

func handleSends(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.sending", "kannon-stats-sending-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			sendMsg := pb.EmailToSend{}
			err = proto.Unmarshal(msg.Data, &sendMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[🚀 Prepared] %v %v", sendMsg.To, sendMsg.MessageId)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(sendMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertPrepared(ctx, sq.InsertPreparedParams{
					Email:     sendMsg.To,
					MessageID: msgID,
					Timestamp: time.Now(),
					Domain:    domain,
				})
				if err != nil {
					logrus.Errorf("Cannot insert sent: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
			}
		}
	}
}

func handleErrors(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.error", "kannon-stats-error-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			errMsg := pb.Error{}
			err = proto.Unmarshal(msg.Data, &errMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[🛑 bounce] %v %v - %v", errMsg.Email, errMsg.MessageId, errMsg.Msg)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(errMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				storeBounce(ctx, q, errMsg.Email, msgID, domain, errMsg.Timestamp.AsTime(), &types.StatsDataBounced{
					Permanent: errMsg.IsPermanent,
					Code:      errMsg.Code,
					Msg:       errMsg.Msg,
				})
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
			}
		}
	}
}

func handleSoftBounce(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.soft-bounce", "kannon-stats-soft-bounce-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			sMsg := pb.SoftBounce{}
			err = proto.Unmarshal(msg.Data, &sMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[🤷 soft bounce] %v %v - %d", sMsg.Email, sMsg.MessageId, sMsg.Code)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(sMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				storeBounce(ctx, q, sMsg.Email, msgID, domain, sMsg.Timestamp.AsTime(), &types.StatsDataBounced{
					Permanent: false,
					Code:      sMsg.Code,
					Msg:       sMsg.Msg,
				})
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}

func handleDelivereds(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.delivered", "kannon-stats-delivered-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			deliveredMsg := pb.Delivered{}
			err = proto.Unmarshal(msg.Data, &deliveredMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[✅ delivered] %v %v", deliveredMsg.Email, deliveredMsg.MessageId)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(deliveredMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				storeDelivered(ctx, q, deliveredMsg.Email, msgID, domain, deliveredMsg.Timestamp.AsTime(), &types.StatsDataDelivered{})
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}

func handleOpens(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.opens", "kannon-stats-opens-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			oMsg := pb.Open{}
			err = proto.Unmarshal(msg.Data, &oMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[👀 open] %v %v", oMsg.Email, oMsg.MessageId)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(oMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				storeOpened(ctx, q, oMsg.Email, msgID, domain, oMsg.Timestamp.AsTime(), &types.StatsDataOpened{
					UserAgent: oMsg.UserAgent,
					Ip:        oMsg.Ip,
				})
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}

func handleClicks(ctx context.Context, js nats.JetStreamContext, q *sq.Queries) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.clicks", "kannon-stats-clicks-logs")
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			cMsg := pb.Click{}
			err = proto.Unmarshal(msg.Data, &cMsg)
			if err != nil {
				logrus.Errorf("cannot marshal message %v", err.Error())
			} else {
				logrus.Printf("[🔗 click] %v %v - %v", cMsg.Email, cMsg.MessageId, cMsg.Url)
				msgID, domain, err := utils.ExtractMsgIDAndDomain(cMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgID and domain: %v", err)
					msg.Ack()
					continue
				}
				storeClicked(ctx, q, cMsg.Email, msgID, domain, cMsg.Timestamp.AsTime(), &types.StatsDataClicked{
					UserAgent: cMsg.UserAgent,
					Ip:        cMsg.Ip,
					Url:       cMsg.Url,
				})
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}

func storeBounce(ctx context.Context, q *sq.Queries, email, messageID, domain string, timestamp time.Time, data *types.StatsDataBounced) {
	storeStat(ctx, q, sq.StatsTypeBounce, email, messageID, domain, timestamp, &types.StatsData{
		Data: &types.StatsData_Bounced{
			Bounced: data,
		},
	})
}

func storeDelivered(ctx context.Context, q *sq.Queries, email, messageID, domain string, timestamp time.Time, data *types.StatsDataDelivered) {
	storeStat(ctx, q, sq.StatsTypeDelivered, email, messageID, domain, timestamp, &types.StatsData{
		Data: &types.StatsData_Delivered{
			Delivered: data,
		},
	})
}

func storeOpened(ctx context.Context, q *sq.Queries, email, messageID, domain string, timestamp time.Time, data *types.StatsDataOpened) {
	storeStat(ctx, q, sq.StatsTypeOpened, email, messageID, domain, timestamp, &types.StatsData{
		Data: &types.StatsData_Opened{
			Opened: data,
		},
	})
}

func storeClicked(ctx context.Context, q *sq.Queries, email, messageID, domain string, timestamp time.Time, data *types.StatsDataClicked) {
	storeStat(ctx, q, sq.StatsTypeClicked, email, messageID, domain, timestamp, &types.StatsData{
		Data: &types.StatsData_Clicked{
			Clicked: data,
		},
	})
}

func storeStat(ctx context.Context, q *sq.Queries, stype sq.StatsType, email, messageID, domain string, timestamp time.Time, data *types.StatsData) {
	err := q.InsertStat(ctx, sq.InsertStatParams{
		Email:     email,
		MessageID: messageID,
		Timestamp: timestamp,
		Domain:    domain,
		Type:      stype,
		Data:      data,
	})
	if err != nil {
		logrus.Errorf("Cannot insert %v stat: %v", stype, err)
	}
}
