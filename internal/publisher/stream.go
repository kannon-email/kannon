package publisher

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// SendingStreamName is the JetStream stream Envelopes wait on to be transmitted.
	SendingStreamName = "kannon-sending"
	// SendingSubject is the subject SendEmail publishes Envelopes on.
	SendingSubject = "kannon.sending"
)

// ConfigureSendingStream ensures the sending stream exists. The one place this stream's
// configuration is written down.
func ConfigureSendingStream(ctx context.Context, sc utils.StreamCreator) error {
	return utils.ConfigureStream(ctx, sc, jetstream.StreamConfig{
		Name:        SendingStreamName,
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{SendingSubject},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	})
}
