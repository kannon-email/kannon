package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"kannon.gyozatech.dev/internal/db"
	"kannon.gyozatech.dev/internal/mailbuilder"
	"kannon.gyozatech.dev/internal/pool"

	"github.com/nats-io/nats.go"
)

type appConfig struct {
	NatsConn string `default:"nats://127.0.0.1:4222"`
}

func main() {
	godotenv.Load()

	var config appConfig
	err := envconfig.Process("app", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	dbi, err := db.NewDb(true)
	if err != nil {
		panic(err)
	}

	pm, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		panic(err)
	}

	mb := mailbuilder.NewMailBuilder(dbi)

	nc, err := nats.Connect(config.NatsConn)
	if err != nil {
		logrus.Fatalf("Cannot connect to nats: %v\n", err)
	}

	for {
		emails, err := pm.PrepareForSend(100)
		if err != nil {
			logrus.Fatalf("cannot prepare for send: %v\n", err)
		}
		logrus.Infof("Fetched %v emails\n", len(emails))
		for _, email := range emails {
			data, err := mb.PerpareForSend(email)
			if err != nil {
				logrus.Errorf("Cannot send email %v: %v\n", email.To, err)
			}
			msg, err := proto.Marshal(&data)
			if err != nil {
				logrus.Errorf("Cannot send email %v: %v\n", email.To, err)
			}
			nc.Publish("emails.sending", msg)
		}
		logrus.Infof("done sending emails\n")
		time.Sleep(1 * time.Second)
	}
}
