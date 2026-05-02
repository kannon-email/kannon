package publisher

import (
	"fmt"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type Publisher interface {
	Publish(subj string, data []byte) error
}

// SendEmail translates a domain Envelope to its on-the-wire EmailToSend
// proto and publishes it on the kannon.sending subject. The proto type
// boundary lives here so the rest of the dispatcher / builder pipeline
// stays in domain types.
func SendEmail(p Publisher, env *envelope.Envelope) error {
	logrus.WithField("subj", "kannon.sending").Debugf("[nats] publishing message")
	msg, err := proto.Marshal(env.ToProto())
	if err != nil {
		return err
	}
	err = p.Publish("kannon.sending", msg)
	if err != nil {
		return err
	}
	return nil
}

func PublishStat(p Publisher, s *types.Stats) error {
	stype := stats.DetermineTypeFromStats(s)
	subj := fmt.Sprintf("kannon.stats.%s", stype)

	data, err := proto.Marshal(s)
	if err != nil {
		return fmt.Errorf("cannot marshal protoc: %v", err)
	}
	return p.Publish(subj, data)
}
