package utils

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
)

func MustGetPullSubscriber(ctx context.Context, js jetstream.JetStream, stream string, subj string, durable string) jetstream.Consumer {
	var lastErr error

	for i := 0; i < 10; i++ {
		conn, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
			Name:          durable,
			Durable:       durable,
			FilterSubject: subj,
		})
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
