package sqlc

import "github.com/kannon-email/kannon/internal/stats"

// StatsData is the JSONB payload of the stats.data column: what one Delivery
// outcome looks like on disk, stated by Kannon rather than inherited from a wire
// format. Until it existed the column was overridden onto the generated
// kannon.stats.types.StatsData and encoded by two hand-written methods on that
// generated package, so the storage format of a Kannon table was defined by a
// published .proto whose compatibility rules do not describe storage: a field
// rename is safe on the wire, because the binary encoding keys on tag numbers,
// and would have turned every row already written into a read error (ADR 0012).
//
// The encoding is a faithful reproduction of what protojson emitted — the
// variant as a single lowerCamelCase key, zero-valued scalars omitted entirely,
// so {"bounced":{}} is a real and valid stored bounce — because reproducing it
// is exactly what lets the boundary be introduced with no migration of a table
// that holds a row per Delivery outcome for as long as the retention allows.
// What is bought even so is that the shape is now stated in struct tags in one
// file, so the next person to change it is changing a storage format and can see
// that they are, and the day it is worth migrating it is a change to this type
// and a backfill rather than to a schema clients have built against.
//
// The column is JSONB NOT NULL and this type is therefore stored by value: an
// outcome no build can name is {}, which is what an unset protobuf oneof always
// rendered as, rather than the SQL NULL a nil pointer would have encoded to and
// the column would have refused.
//
// At most one variant is ever set. It is eight nullable pointers rather than a
// tag and a union because that is precisely the JSON document being described,
// and the domain type this maps to and from — stats.Outcome — is the place where
// "exactly one of these" is enforced by construction.
type StatsData struct {
	Accepted  *StatsDataAccepted  `json:"accepted,omitempty"`
	Delivered *StatsDataDelivered `json:"delivered,omitempty"`
	Failed    *StatsDataFailed    `json:"failed,omitempty"`
	Bounced   *StatsDataBounced   `json:"bounced,omitempty"`
	Opened    *StatsDataOpened    `json:"opened,omitempty"`
	Clicked   *StatsDataClicked   `json:"clicked,omitempty"`
	Rejected  *StatsDataRejected  `json:"rejected,omitempty"`
	Error     *StatsDataError     `json:"error,omitempty"`
}

// StatsDataAccepted carries nothing: the Validator having accepted an address is
// the whole of the event. It is a struct rather than a bare bool so that the key
// is present with an empty object, as protojson wrote it.
type StatsDataAccepted struct{}

// StatsDataDelivered likewise carries nothing. The remote MX's reply text is not
// retained; that a next hop took responsibility is the event.
type StatsDataDelivered struct{}

// StatsDataRejected carries Kannon's own account of why a Recipient was refused.
type StatsDataRejected struct {
	Reason string `json:"reason,omitempty"`
}

// StatsDataFailed carries a reason and deliberately no reply code: a Delivery
// that ran out its Retry Budget without a single attempt ever being answered has
// no reply to quote, and that absence is what separates Failed from Bounced
// (CONTEXT.md, ADR 0007).
type StatsDataFailed struct {
	Reason string `json:"reason,omitempty"`
}

// StatsDataBounced quotes the reply a remote mail system gave. Permanent is a
// classification of that reply by SMTP class and not of the retry decision that
// led here (#378, #433).
type StatsDataBounced struct {
	Permanent bool   `json:"permanent,omitempty"`
	Code      uint32 `json:"code,omitempty"`
	Msg       string `json:"msg,omitempty"`
}

// StatsDataError is the transient retry signal CONTEXT.md keeps out of the
// shared language. It is stored because it travels the kannon.stats.* path
// today, not because it is an outcome of the Delivery.
type StatsDataError struct {
	Code uint32 `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

// StatsDataOpened holds the request detail retained under the Full Tracking Mode
// only; under every lower Mode both fields are empty because nothing was
// retained, and both are then omitted from the stored document.
//
// The JSON name is userAgent and not user_agent. That is protojson's rendering
// of the proto field user_agent, and rows carrying it have been written since
// the column existed.
type StatsDataOpened struct {
	UserAgent string `json:"userAgent,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// StatsDataClicked is Opened plus the URL that was followed.
type StatsDataClicked struct {
	UserAgent string `json:"userAgent,omitempty"`
	IP        string `json:"ip,omitempty"`
	URL       string `json:"url,omitempty"`
}

// StatsDataFromOutcome renders a domain Outcome in the shape the column stores.
// An Outcome this build cannot name renders as the empty document, which is both
// what the column will accept and what such a payload has always looked like.
func StatsDataFromOutcome(o stats.Outcome) StatsData {
	switch o.Type() {
	case stats.TypeAccepted:
		return StatsData{Accepted: &StatsDataAccepted{}}
	case stats.TypeDelivered:
		return StatsData{Delivered: &StatsDataDelivered{}}
	case stats.TypeRejected:
		return StatsData{Rejected: &StatsDataRejected{Reason: o.Reason()}}
	case stats.TypeFailed:
		return StatsData{Failed: &StatsDataFailed{Reason: o.Reason()}}
	case stats.TypeBounce:
		return StatsData{Bounced: &StatsDataBounced{
			Permanent: o.Permanent(),
			Code:      o.Code(),
			Msg:       o.Msg(),
		}}
	case stats.TypeError:
		return StatsData{Error: &StatsDataError{Code: o.Code(), Msg: o.Msg()}}
	case stats.TypeOpened:
		return StatsData{Opened: &StatsDataOpened{UserAgent: o.UserAgent(), IP: o.IP()}}
	case stats.TypeClicked:
		return StatsData{Clicked: &StatsDataClicked{
			UserAgent: o.UserAgent(),
			IP:        o.IP(),
			URL:       o.URL(),
		}}
	default:
		return StatsData{}
	}
}

// Outcome reads a stored payload back into the domain. An empty document and a
// document naming a variant this build does not know both read as the zero
// Outcome, which is TypeUnknown — the same answer the protojson path gave for an
// unset oneof.
//
// The reader is deliberately lenient about keys it does not recognise, where the
// protojson.Unmarshal it replaces was called with no options and so refused them
// outright. That is a real change of behaviour and it is the one wanted: a
// storage decoder that fails a page of Stats because a newer build wrote a
// variant it has never heard of is worse than one that reports what it can read,
// and nothing is actually lost by tolerating the unknown here, because the kind
// of an event is recorded independently in the stats.type column and LoadStat
// reports the row's type from there rather than deriving it from this document.
// Refusing unknown keys would only be worth its cost if this were the sole
// record of what the event was.
//
// Nothing writes more than one variant, so the order of these tests is only a
// tie-break for a document no version of Kannon has ever produced.
func (d StatsData) Outcome() stats.Outcome {
	switch {
	case d.Accepted != nil:
		return stats.Accepted()
	case d.Delivered != nil:
		return stats.Delivered()
	case d.Failed != nil:
		return stats.Failed(d.Failed.Reason)
	case d.Bounced != nil:
		return stats.Bounced(d.Bounced.Permanent, d.Bounced.Code, d.Bounced.Msg)
	case d.Opened != nil:
		return stats.Opened(d.Opened.UserAgent, d.Opened.IP)
	case d.Clicked != nil:
		return stats.Clicked(d.Clicked.UserAgent, d.Clicked.IP, d.Clicked.URL)
	case d.Rejected != nil:
		return stats.Rejected(d.Rejected.Reason)
	case d.Error != nil:
		return stats.Errored(d.Error.Code, d.Error.Msg)
	default:
		return stats.Outcome{}
	}
}
