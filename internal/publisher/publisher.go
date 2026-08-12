package publisher

import (
	"fmt"
	"log/slog"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/envelopepb"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statspb"
)

type Publisher interface {
	Publish(subj string, data []byte) error
}

// SendEmail publishes a domain Envelope on the kannon.sending subject. Naming
// the subject the SMTPSender's consumer filters on is what this package keeps;
// the wire form lives in internal/envelopepb, next to the read side the
// SMTPSender uses, so the two ends of this topic cannot drift apart (ADR 0012).
func SendEmail(p Publisher, env *envelope.Envelope) error {
	slog.Debug("[nats] publishing message", "subj", "kannon.sending")
	msg, err := envelopepb.MarshalEnvelope(env)
	if err != nil {
		return err
	}
	err = p.Publish("kannon.sending", msg)
	if err != nil {
		return err
	}
	return nil
}

// PublishStat publishes a domain stat Event on the topic named after the
// Outcome it carries. The subject and the payload are derived from the same
// value here and nowhere else, which is what stops a producer naming a topic no
// consumer subscribes to (#376).
func PublishStat(p Publisher, e stats.Event) error {
	subj := fmt.Sprintf("kannon.stats.%s", e.Outcome.Type())

	data, err := statspb.MarshalEvent(e)
	if err != nil {
		return fmt.Errorf("cannot marshal protoc: %w", err)
	}
	return p.Publish(subj, data)
}
