package utils

import (
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

func MustGetPullSubscriber(js nats.JetStreamContext, subj string, durable string, opts ...nats.SubOpt) *nats.Subscription {
	conn, err := js.PullSubscribe(subj, durable, opts...)
	if err != nil {
		logrus.Fatalf("cannot create pull subscriber %v: %v", durable, err)
	}
	return conn
}
