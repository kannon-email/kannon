package stats

import (
	"context"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"

	"github.com/ludusrusso/kannon/generated/pb"
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
				msgId, domain, err := utils.ExtractMsgIDAndDomain(sendMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgId and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertPrepared(ctx, sq.InsertPreparedParams{
					Email:     sendMsg.To,
					MessageID: msgId,
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
				msgId, domain, err := utils.ExtractMsgIDAndDomain(errMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgId and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertHardBounced(ctx, sq.InsertHardBouncedParams{
					Email:     errMsg.Email,
					MessageID: msgId,
					Timestamp: errMsg.Timestamp.AsTime(),
					Domain:    domain,
					ErrCode:   int32(errMsg.Code),
					ErrMsg:    errMsg.Msg,
				})
				if err != nil {
					logrus.Errorf("Cannot insert bounced: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
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
				msgId, domain, err := utils.ExtractMsgIDAndDomain(deliveredMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgId and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertAccepted(ctx, sq.InsertAcceptedParams{
					Email:     deliveredMsg.Email,
					MessageID: msgId,
					Timestamp: deliveredMsg.Timestamp.AsTime(),
					Domain:    domain,
				})
				if err != nil {
					logrus.Errorf("Cannot insert delivered: %v", err)
				}
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
				msgId, domain, err := utils.ExtractMsgIDAndDomain(oMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgId and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertOpen(ctx, sq.InsertOpenParams{
					Email:     oMsg.Email,
					MessageID: msgId,
					Timestamp: oMsg.Timestamp.AsTime(),
					Domain:    domain,
					Ip:        oMsg.Ip,
					UserAgent: oMsg.UserAgent,
				})
				if err != nil {
					logrus.Errorf("Cannot insert open: %v", err)
				}
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
				msgId, domain, err := utils.ExtractMsgIDAndDomain(cMsg.MessageId)
				if err != nil {
					logrus.Errorf("Cannot extract msgId and domain: %v", err)
					msg.Ack()
					continue
				}
				err = q.InsertClick(ctx, sq.InsertClickParams{
					Email:     cMsg.Email,
					MessageID: msgId,
					Timestamp: cMsg.Timestamp.AsTime(),
					Domain:    domain,
					Ip:        cMsg.Ip,
					UserAgent: cMsg.UserAgent,
					Url:       cMsg.Url,
				})
				if err != nil {
					logrus.Errorf("Cannot insert click: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
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
				err = q.InsertSoftBounce(ctx, sq.InsertSoftBounceParams{
					Email:     sMsg.Email,
					MessageID: sMsg.MessageId,
					Timestamp: sMsg.Timestamp.AsTime(),
					Domain:    sMsg.Domain,
					Code:      int32(sMsg.Code),
					Msg:       sMsg.Msg,
				})
				if err != nil {
					logrus.Errorf("Cannot insert soft bounce: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}
