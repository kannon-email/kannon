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

// AckPolicy is how long a consumer is given to settle a delivery before the
// JetStream server hands the same message to somebody else, attempt by
// attempt: entry i is the deadline of attempt i+1, and equally the delay
// before attempt i+2 goes out. The last entry governs every further attempt,
// up to consumerMaxDeliver.
//
// One curve, and not a base deadline plus a backoff, because JetStream
// conflates the two: a non-empty BackOff "overrides AckWait" (see the doc
// comment on jetstream.ConsumerConfig.BackOff) — the server rewrites AckWait
// to BackOff[0] when it creates the consumer, and reuses the last entry of the
// curve once the curve runs out. An AckWait set next to a BackOff therefore
// says nothing at all: that is how the sending consumer came to have one
// second to acknowledge an SMTP transaction, and to physically re-send every
// email slower than that (#425).
type AckPolicy []time.Duration

// FirstDeadline is the deadline of a first delivery. It is the one that
// matters: every later attempt is by definition a redelivery, so this is the
// only deadline that applies while the work is being done for the first time.
func (p AckPolicy) FirstDeadline() time.Duration {
	return p[0]
}

// applyTo writes the policy onto a consumer config, keeping AckWait and
// BackOff in agreement so the config states the deadline it actually gets.
func (p AckPolicy) applyTo(cfg *jetstream.ConsumerConfig) {
	cfg.AckWait = p.FirstDeadline()
	cfg.BackOff = p
}

// DefaultAckPolicy suits a consumer whose handler is a local unit of work —
// a database round trip or two — and which therefore acks within
// milliseconds. It reacts quickly and grows fast, so a consumer stuck on a
// single message backs off to once per five minutes within a handful of
// attempts (#396).
//
// A handler that can legitimately hold a message for longer than the first
// entry must say so with WithAckPolicy, or the server will hand its message to
// another worker while it is still being worked on.
var DefaultAckPolicy = AckPolicy{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
}

type consumerOptions struct {
	ack AckPolicy
}

// ConsumerOption customises the consumer created by MustGetPullSubscriber.
type ConsumerOption func(*consumerOptions)

// WithAckPolicy gives this consumer its own ack deadline curve in place of
// DefaultAckPolicy. An empty policy is ignored.
func WithAckPolicy(p AckPolicy) ConsumerOption {
	return func(o *consumerOptions) {
		if len(p) == 0 {
			return
		}
		o.ack = p
	}
}

func MustGetPullSubscriber(ctx context.Context, js jetstream.JetStream, stream string, subj string, durable string, opts ...ConsumerOption) jetstream.Consumer {
	o := consumerOptions{ack: DefaultAckPolicy}
	for _, opt := range opts {
		opt(&o)
	}

	cfg := jetstream.ConsumerConfig{
		Name:          durable,
		Durable:       durable,
		FilterSubject: subj,
		MaxDeliver:    consumerMaxDeliver,
	}
	o.ack.applyTo(&cfg)

	var lastErr error

	for range 10 {
		conn, err := js.CreateOrUpdateConsumer(ctx, stream, cfg)
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
