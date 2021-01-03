package main

import (
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/mailer"
	"smtp.ludusrusso.space/internal/pool"
	"smtp.ludusrusso.space/internal/smtp"
)

func main() {
	godotenv.Load()

	dbi, err := db.NewDb(true)
	sender := smtp.NewSender("mail.ludusrusso.space")
	ms := mailer.NewSMTPMailer(sender, dbi)

	if err != nil {
		logrus.Fatalf("cannot create db: %v\n", err)
	}

	pm, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		logrus.Fatalf("cannot create sending pool manager: %v\n", err)
	}

	for {
		emails, err := pm.PrepareForSend(100)
		if err != nil {
			logrus.Fatalf("cannot prepare for send: %v\n", err)
		}
		logrus.Infof("Fetched %v emails\n", len(emails))

		wg := new(sync.WaitGroup)

		wg.Add(len(emails))
		for _, email := range emails {
			go func(email db.SendingPoolEmail) {
				ms.Send(email)
				wg.Done()
			}(email)
		}
		wg.Wait()
		logrus.Infof("done sending emails\n")

		time.Sleep(1 * time.Second)
	}
}
