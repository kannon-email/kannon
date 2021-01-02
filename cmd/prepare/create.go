package main

import (
	"fmt"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/pool"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		logrus.Fatal("Error loading .env file")
	}

	fmt.Printf("Ciao\n")
	dbi, err := db.NewDb(true)
	if err != nil {
		logrus.Fatalf("cannot create db: %v\n", err)
	}

	// dm, err := domains.NewDomainManager(db)
	// if err != nil {
	// 	logrus.Fatalf("cannot create domain manager: %v\n", err)
	// }

	// domain, err := dm.CreateDomain("mail.ludusrusso.space")
	// if err != nil {
	// 	logrus.Fatalf("cannot create domain: %v\n", err)
	// }
	// logrus.Infof("created domain &v", domain)

	pm, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		logrus.Fatalf("cannot create sending pool manager: %v\n", err)
	}

	template, err := pm.CreateTemplate("test", db.TemplateTypeTmp)
	if err != nil {
		logrus.Fatalf("cannot create template: %v\n", err)
	}
	logrus.Infof("template %v", template)

	pool, err := pm.AddPool(
		template,
		[]string{"ludus.russo@gmail.com", "ludovico@ludusrusso.space"},
		db.Sender{
			Email: "ludovico@ludusrusso.space",
			Alias: "Ludovico Russo",
		}, "Ciao", "mail.ludusrusso.space")
	if err != nil {
		logrus.Fatalf("cannot create pool: %v\n", err)
	}
	logrus.Infof("pool %v", pool)
}
