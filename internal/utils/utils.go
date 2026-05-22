package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// consumerMaxDeliver caps the number of redelivery attempts for any single
// JetStream message routed through MustGetPullSubscriber. Without this cap
// (server default is -1 = unbounded) a poison message paired with a Nak()
// loop saturates a CPU core.
const consumerMaxDeliver = 100

// consumerAckWait is the base ack timeout. It is also the redelivery delay
// applied after consumerBackOff is exhausted.
const consumerAckWait = 30 * time.Second

// consumerBackOff is the per-attempt redelivery delay curve used by the
// JetStream server when a message is Nak'd or its AckWait elapses. The
// curve grows quickly so any consumer stuck on a single message backs off
// to once-per-five-minutes within a handful of attempts.
var consumerBackOff = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
}

func MustGetPullSubscriber(ctx context.Context, js jetstream.JetStream, stream string, subj string, durable string) jetstream.Consumer {
	var lastErr error

	for range 10 {
		conn, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
			Name:          durable,
			Durable:       durable,
			FilterSubject: subj,
			MaxDeliver:    consumerMaxDeliver,
			AckWait:       consumerAckWait,
			BackOff:       consumerBackOff,
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
