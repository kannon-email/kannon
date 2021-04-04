package main

import (
	"encoding/json"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"kannon.gyozatech.dev/internal/db"
	"kannon.gyozatech.dev/internal/pool"

	"github.com/nats-io/nats.go"
)

func main() {
	godotenv.Load()

	pm, err := newSendingPoolManager()
	if err != nil {
		logrus.Fatalf("Cannot create pool manager: %v\n", err)
	}

	nc, err := nats.Connect(nats.DefaultURL)
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
			err := sendEmail(email, nc, "emails.sending")
			if err != nil {
				logrus.Error("Cannot send email %v: %v\n", email.To, err)
			}
		}
		logrus.Infof("done sending emails\n")
		time.Sleep(1 * time.Second)
	}
}

func sendEmail(email db.SendingPoolEmail, nc *nats.Conn, subj string) error {
	msg, err := json.Marshal(email)
	if err != nil {
		return err
	}
	return nc.Publish(subj, msg)
}

func newSendingPoolManager() (pool.SendingPoolManager, error) {
	dbi, err := db.NewDb(true)
	if err != nil {
		return nil, err
	}

	pm, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		return nil, err
	}
	return pm, nil
}
