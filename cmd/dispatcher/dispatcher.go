package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/mailbuilder"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/nats-io/nats.go"
)

type appConfig struct {
	NatsConn string `default:"nats://0.0.0.0:4222"`
}

func main() {
	_ = godotenv.Load()

	var config appConfig
	err := envconfig.Process("app", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	db, err := sqlc.Conn()
	if err != nil {
		panic(err)
	}
	defer db.Close()

	pm, err := pool.NewSendingPoolManager(db)
	if err != nil {
		panic(err)
	}

	mb := mailbuilder.NewMailBuilder(db)

	logrus.Infof("Connecting to nats... %v", config.NatsConn)

	nc, js, closeNats := utils.MustGetNats(config.NatsConn)
	defer closeNats()
	mustConfigureJS(js)

	ctx := context.Background()

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
				logrus.Errorf("Cannot hack msg to nats: %v\n", err)
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
		logrus.Infof("stream exists\n")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
