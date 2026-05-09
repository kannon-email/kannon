package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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

		slog.Error("cannot create pull subscriber", "durable", durable, "err", err)
		time.Sleep(1 * time.Second)
		lastErr = err
	}

	slog.Error("cannot create pull subscriber", "durable", durable, "err", lastErr)
	os.Exit(1)
	return nil
}
