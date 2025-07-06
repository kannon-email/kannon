package utils

import (
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

func MustGetPullSubscriber(js nats.JetStreamContext, subj string, durable string) *nats.Subscription {
	var lastErr error
	for i := 0; i < 10; i++ {
		conn, err := js.PullSubscribe(subj, durable)
		if err == nil {
			return conn
		}
		logrus.Errorf("cannot create pull subscriber %v: %v", durable, err)
		time.Sleep(1 * time.Second)
		lastErr = err
	}
	logrus.Fatalf("cannot create pull subscriber %v: %v", durable, lastErr)
	return nil
}
