package utils

import (
	"os"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

type CloseFunc = func()

func MustGetNats(natsUrl string) (*nats.Conn, nats.JetStreamContext, CloseFunc) {
	nc, err := nats.Connect(natsUrl)
	if err != nil {
		logrus.Fatalf("cannot create nats cli: %v", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		logrus.Fatalf("cannot create js cli: %v", err)
	}

	var close CloseFunc = func() {
		err := nc.Drain()
		if err != nil {
			logrus.Errorf("cannot drain nats: %v", err)
		}
	}
	return nc, js, close
}

func MustEnv(envName string) string {
	env := os.Getenv(envName)
	if env == "" {
		logrus.Fatalf("%v not defined", envName)
	}
	return env
}

func MustGetPullSubscriber(js nats.JetStreamContext, subj string, durable string, opts ...nats.SubOpt) *nats.Subscription {
	conn, err := js.PullSubscribe(subj, durable, opts...)
	if err != nil {
		logrus.Fatalf("cannot create pull subscriber %v: %v", durable, err)
	}
	return conn
}
