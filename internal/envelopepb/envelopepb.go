// Package envelopepb translates Envelopes between the wire EmailToSend message
// and the internal/envelope domain entity. It is the only place that knows
// both, so that internal/envelope stays free of any protobuf dependency and the
// Dispatcher that publishes an Envelope on kannon.sending and the SMTPSender
// that transmits it work from one field mapping rather than two that happen to
// agree.
//
// The translation is total and never fails. Unlike a value a client states,
// everything here was written by Kannon and published by Kannon moments
// earlier, so there is nothing to refuse: a message this build cannot read is a
// fault for the consumer holding it to report, not an error raised in the
// middle of a field mapping.
package envelopepb

import (
	"github.com/kannon-email/kannon/internal/envelope"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"google.golang.org/protobuf/proto"
)

// MarshalEnvelope renders an Envelope as the bytes published on kannon.sending,
// and UnmarshalEnvelope reads them back. The encoding lives here with the field
// mapping so that this package is the whole answer to how an Envelope reaches
// the wire, and the transport that carries it needs no protobuf dependency of
// its own (ADR 0012).
func MarshalEnvelope(env *envelope.Envelope) ([]byte, error) {
	return proto.Marshal(FromEnvelope(env))
}

// UnmarshalEnvelope reads an Envelope off the wire. Bytes that do not parse are
// reported rather than absorbed: the caller is holding the message and is the
// only thing that can decide whether to retry it or drop it.
func UnmarshalEnvelope(b []byte) (*envelope.Envelope, error) {
	var m pb.EmailToSend
	if err := proto.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return ToEnvelope(&m), nil
}

// FromEnvelope renders an Envelope as the EmailToSend that travels on
// kannon.sending. It reads the entity through its public getters only, so the
// domain entity owes nothing to protobuf.
func FromEnvelope(env *envelope.Envelope) *pb.EmailToSend {
	return &pb.EmailToSend{
		EmailId:     env.EmailID(),
		From:        env.From(),
		To:          env.To(),
		ReturnPath:  env.ReturnPath(),
		Body:        env.Body(),
		ShouldRetry: env.ShouldRetry(),
	}
}

// ToEnvelope reads a published EmailToSend back into the domain, which is what
// the SMTPSender does with every message it takes off the sending stream.
//
// A nil message reads as the zero Envelope rather than as a nil pointer. Nil
// cannot arrive from the wire — the caller unmarshals into a message it
// allocated itself — so what the choice settles is a direct call, and there the
// zero Envelope is the reading that cannot fault: every field of an Envelope is
// reached through a method, so returning nil would only move the failure to
// whichever getter the consumer called first.
func ToEnvelope(m *pb.EmailToSend) *envelope.Envelope {
	return envelope.New(envelope.Params{
		EmailID:     m.GetEmailId(),
		From:        m.GetFrom(),
		To:          m.GetTo(),
		ReturnPath:  m.GetReturnPath(),
		Body:        m.GetBody(),
		ShouldRetry: m.GetShouldRetry(),
	})
}
