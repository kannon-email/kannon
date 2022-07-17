package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/mailbuilder"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go"
)

func Run(ctx context.Context, vc *viper.Viper) {
	vc.SetEnvPrefix("DISPATCHER")
	vc.AutomaticEnv()

	dbURL := vc.GetString("database_url")
	natsURL := vc.GetString("nats_url")

	logrus.Info("🚀 Starting dispatcher")

	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	ss := statssec.NewStatsService(q)
	pm := pool.NewSendingPoolManager(q)
	mb := mailbuilder.NewMailBuilder(q, ss)

	nc, js, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()
	mustConfigureJS(js)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		handleErrors(ctx, js, pm)
		wg.Done()
	}()
	go func() {
		handleDelivereds(ctx, js, pm)
		wg.Done()
	}()
	go func() {
		dispatcherLoop(ctx, pm, mb, nc)
		wg.Done()
	}()
	wg.Wait()
}

func dispatcherLoop(ctx context.Context, pm pool.SendingPoolManager, mb mailbuilder.MailBulder, nc *nats.Conn) {
	for {
		if err := dispatch(ctx, pm, mb, nc); err != nil {
			logrus.Errorf("cannot dispatch: %v", err)
		}
		time.Sleep(1 * time.Second)
	}
}

func dispatch(pctx context.Context, pm pool.SendingPoolManager, mb mailbuilder.MailBulder, nc *nats.Conn) error {
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	emails, err := pm.PrepareForSend(ctx, 100)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}
	for _, email := range emails {
		data, err := mb.PerpareForSend(ctx, email)
		if err != nil {
			logrus.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		msg, err := proto.Marshal(&data)
		if err != nil {
			logrus.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		err = nc.Publish("kannon.sending", msg)
		if err != nil {
			logrus.Errorf("Cannot send message on nats: %v", err)
			continue
		}
		logrus.Infof("[✅ accepted]: %v %v", data.To, data.MessageId)
	}
	logrus.Debugf("done sending emails")
	return nil
}

func handleErrors(ctx context.Context, js nats.JetStreamContext, pm pool.SendingPoolManager) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.error", "kannon-stats-error")
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
				logrus.Printf("[🛑 bump] %v %v - %v", errMsg.Email, errMsg.MessageId, errMsg.Msg)
				if err := pm.SetError(ctx, errMsg.MessageId, errMsg.Email, errMsg.Msg); err != nil {
					logrus.Errorf("Cannot set delivered: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
			}
		}
	}
}

func handleDelivereds(ctx context.Context, js nats.JetStreamContext, pm pool.SendingPoolManager) {
	con := utils.MustGetPullSubscriber(js, "kannon.stats.delivered", "kannon-stats-delivered")
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
				logrus.Printf("[🚀 delivered] %v %v", deliveredMsg.Email, deliveredMsg.MessageId)
				if err := pm.SetDelivered(ctx, deliveredMsg.MessageId, deliveredMsg.Email); err != nil {
					logrus.Errorf("Cannot set delivered: %v", err)
				}
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("Cannot hack msg to nats: %v", err)
			}
		}
	}
}

func mustConfigureJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-sending",
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.sending"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
