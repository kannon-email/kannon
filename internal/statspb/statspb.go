// Package statspb translates stat Events and Outcomes between the wire messages
// and the internal/stats domain types. It is the only place that knows both, so
// that internal/stats stays free of any protobuf dependency and every producer
// on the kannon.stats.* topics — Validator, Dispatcher, SMTPSender, SMTPServer,
// Tracker — puts the same field mapping on the wire that the Stats worker and
// the Dispatcher's consumers read back off it.
//
// The translation is total and never fails. Unlike a Tracking Policy a client
// states, everything here was written by Kannon itself, so a payload this build
// cannot read is a fault to be reported by the consumer that has the message in
// hand and can Term it — not an error raised in the middle of a field mapping.
// An unreadable payload reads as the zero Outcome, which is TypeUnknown.
package statspb

import (
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/trackingpb"
	pbtypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MarshalEvent renders an Event as the bytes published on a kannon.stats.*
// topic, and UnmarshalEvent reads them back. The encoding lives here with the
// field mapping so that this package is the whole answer to how a stat event
// reaches the wire, and the transport that carries it needs no protobuf
// dependency of its own (ADR 0012).
func MarshalEvent(e stats.Event) ([]byte, error) {
	return proto.Marshal(FromEvent(e))
}

// UnmarshalEvent reads an Event off the wire. A payload that does not parse is
// reported rather than absorbed, because the caller is holding the message and
// is the only thing that can decide its fate — every consumer Terms it, since
// bytes that failed to parse once will fail again and redelivering them is the
// #396 hot loop.
func UnmarshalEvent(b []byte) (stats.Event, error) {
	var m pbtypes.Stats
	if err := proto.Unmarshal(b, &m); err != nil {
		return stats.Event{}, err
	}
	return ToEvent(&m), nil
}

// FromOutcome renders an Outcome as the StatsData that travels on the wire and
// is stored in the stats.data column. The zero Outcome renders as nil, which is
// what an event carrying no payload has always looked like.
func FromOutcome(o stats.Outcome) *pbtypes.StatsData {
	switch o.Type() {
	case stats.TypeAccepted:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Accepted{
			Accepted: &pbtypes.StatsDataAccepted{},
		}}
	case stats.TypeDelivered:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Delivered{
			Delivered: &pbtypes.StatsDataDelivered{},
		}}
	case stats.TypeRejected:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Rejected{
			Rejected: &pbtypes.StatsDataRejected{Reason: o.Reason()},
		}}
	case stats.TypeFailed:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Failed{
			Failed: &pbtypes.StatsDataFailed{Reason: o.Reason()},
		}}
	case stats.TypeBounce:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Bounced{
			Bounced: &pbtypes.StatsDataBounced{
				Permanent: o.Permanent(),
				Code:      o.Code(),
				Msg:       o.Msg(),
			},
		}}
	case stats.TypeError:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Error{
			Error: &pbtypes.StatsDataError{Code: o.Code(), Msg: o.Msg()},
		}}
	case stats.TypeOpened:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Opened{
			Opened: &pbtypes.StatsDataOpened{UserAgent: o.UserAgent(), Ip: o.IP()},
		}}
	case stats.TypeClicked:
		return &pbtypes.StatsData{Data: &pbtypes.StatsData_Clicked{
			Clicked: &pbtypes.StatsDataClicked{
				UserAgent: o.UserAgent(),
				Ip:        o.IP(),
				Url:       o.URL(),
			},
		}}
	default:
		return nil
	}
}

// ToOutcome reads a StatsData back into the domain. A nil payload, an unset
// oneof and a variant this build does not know all read as the zero Outcome:
// each of the three is an event whose outcome this build cannot name, and
// telling them apart would only give a consumer three ways to say the same
// thing.
func ToOutcome(d *pbtypes.StatsData) stats.Outcome {
	switch v := d.GetData().(type) {
	case *pbtypes.StatsData_Accepted:
		return stats.Accepted()
	case *pbtypes.StatsData_Delivered:
		return stats.Delivered()
	case *pbtypes.StatsData_Rejected:
		return stats.Rejected(v.Rejected.GetReason())
	case *pbtypes.StatsData_Failed:
		return stats.Failed(v.Failed.GetReason())
	case *pbtypes.StatsData_Bounced:
		return stats.Bounced(v.Bounced.GetPermanent(), v.Bounced.GetCode(), v.Bounced.GetMsg())
	case *pbtypes.StatsData_Error:
		return stats.Errored(v.Error.GetCode(), v.Error.GetMsg())
	case *pbtypes.StatsData_Opened:
		return stats.Opened(v.Opened.GetUserAgent(), v.Opened.GetIp())
	case *pbtypes.StatsData_Clicked:
		return stats.Clicked(v.Clicked.GetUserAgent(), v.Clicked.GetIp(), v.Clicked.GetUrl())
	default:
		return stats.Outcome{}
	}
}

// FromEvent renders an Event as the Stats message published on kannon.stats.*.
// The subject it is published under is derived from the same Outcome, in
// internal/publisher, so the two cannot disagree (#376).
func FromEvent(e stats.Event) *pbtypes.Stats {
	return &pbtypes.Stats{
		MessageId:    e.MessageID,
		Domain:       e.Domain,
		Email:        e.Email,
		Timestamp:    timestamppb.New(e.Timestamp),
		Type:         legacyTypeField(e.Outcome.Type()),
		Data:         FromOutcome(e.Outcome),
		TrackingMode: trackingpb.FromMode(e.TrackingMode),
	}
}

// ToEvent reads a published Stats message back into the domain. Stats.type is
// ignored, as it always has been: a consumer takes the kind of event from the
// payload, which is the only field a producer is obliged to get right.
func ToEvent(s *pbtypes.Stats) stats.Event {
	return stats.Event{
		MessageID:    s.GetMessageId(),
		Domain:       s.GetDomain(),
		Email:        s.GetEmail(),
		Timestamp:    s.GetTimestamp().AsTime(),
		Outcome:      ToOutcome(s.GetData()),
		TrackingMode: trackingpb.ToMode(s.GetTrackingMode()),
	}
}

// legacyTypeField reproduces the only producer that has ever filled Stats.type:
// the Tracker has stamped it on Opened and Clicked since #420, and no other
// producer has ever set it. Nothing reads it back — every consumer types an
// event off its payload — so the field is dead weight either way, and the
// asymmetry is reproduced rather than tidied up only because this refactor may
// not change a byte on the wire. Dropping it is a wire change and belongs in
// its own commit.
func legacyTypeField(t stats.Type) string {
	switch t {
	case stats.TypeOpened, stats.TypeClicked:
		return string(t)
	default:
		return ""
	}
}
