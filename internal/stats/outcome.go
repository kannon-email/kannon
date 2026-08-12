package stats

// Outcome is what happened to one Delivery — one of the events CONTEXT.md lists
// under "Outcomes (per Delivery)", together with whatever that particular event
// has to say about itself. It is the shape every producer and consumer inside
// Kannon states an outcome in; the protobuf StatsData it travels as is built
// from one in internal/statspb, so nothing on this side of the boundary needs
// to know the wire exists.
//
// It is a flat struct with a discriminant rather than an interface-based sum
// type. The set of outcomes is closed and settled in CONTEXT.md, each variant
// is a handful of scalars, and what a reader does with one is switch on which
// it is — none of which an interface would make clearer, while it would make
// copying, comparing and storing one harder.
//
// Every field is a scalar on purpose: an Outcome is a value, so copying a Stat
// copies its Outcome and no caller can reach back into one it handed on.
type Outcome struct {
	typ Type

	// reason is what Rejected and Failed have to say, msg what Bounced and
	// Errored have. They are separate fields because they are not the same
	// thing: a reason is Kannon's own account of a Delivery it gave up on, a msg
	// is a remote mail system quoted back. The wire keeps them apart too.
	reason string
	msg    string

	permanent bool
	code      uint32

	userAgent string
	ip        string
	url       string
}

// Accepted is the Validator having accepted the recipient address. CONTEXT.md
// calls this outcome Validated; "accepted" is the legacy spelling the wire and
// the stats.type column still use, and renaming those is a breaking change of
// its own (docs/REFACTORING.md §2).
func Accepted() Outcome {
	return Outcome{typ: TypeAccepted}
}

// Delivered is the remote MX having accepted the SMTP handoff. It says nothing
// about an inbox — only that the next hop took responsibility — and a DSN
// arriving later can still Bounce a Delivery that reached it.
func Delivered() Outcome {
	return Outcome{typ: TypeDelivered}
}

// Rejected is a Recipient refused with no Delivery attempted. The reason is
// customer-visible through the stats API, so a caller must keep it to what was
// refused and why.
func Rejected(reason string) Outcome {
	return Outcome{typ: TypeRejected, reason: reason}
}

// Failed is a Delivery whose Retry Budget ran out without a single attempt ever
// being answered. It deliberately carries no reply code: there is none to carry,
// because no remote mail system ever spoke, and that absence is exactly what
// separates Failed from Bounced (CONTEXT.md, ADR 0007).
func Failed(reason string) Outcome {
	return Outcome{typ: TypeFailed, reason: reason}
}

// Bounced is a terminal delivery failure with a reply behind it, whether the
// remote MX rejected during transmission or a DSN arrived later.
//
// permanent qualifies *why* the Delivery is terminal, by SMTP reply class and
// never by the retry decision that led here: 5xx means the address itself is
// dead and worth writing off, 4xx means somebody gave up after retrying — us on
// the synchronous path, the remote MTA on the asynchronous one (#378, #433).
func Bounced(permanent bool, code uint32, msg string) Outcome {
	return Outcome{typ: TypeBounce, permanent: permanent, code: code, msg: msg}
}

// Errored is a transient transmission failure, which triggers a reschedule with
// backoff.
//
// It is the one member of this set that is not an outcome of the Delivery at
// all: CONTEXT.md keeps Errored out of the shared language and flags it for
// demotion to internal logging, because it is a retry signal and nothing more —
// plumbing that happens to travel the kannon.stats.* path today, which is the
// only reason this type has to be able to say it. Nothing customer-facing
// should render one, and a reader looking for what became of a Delivery should
// look past it to the outcome that followed.
func Errored(code uint32, msg string) Outcome {
	return Outcome{typ: TypeError, code: code, msg: msg}
}

// Opened is a tracking pixel having been retrieved. Non-terminal and repeatable.
// The user agent and IP are populated only under the Full Tracking Mode; under
// any lower Mode they are empty because nothing was retained, which is why the
// Event carries the Mode that governed the channel alongside the Outcome.
func Opened(userAgent, ip string) Outcome {
	return Outcome{typ: TypeOpened, userAgent: userAgent, ip: ip}
}

// Clicked is a tracked link having been followed, on the same terms as Opened,
// plus the URL that was followed.
func Clicked(userAgent, ip, url string) Outcome {
	return Outcome{typ: TypeClicked, userAgent: userAgent, ip: ip, url: url}
}

// Type reports which outcome this is. The zero Outcome reports TypeUnknown: an
// event that states no outcome is one this build cannot read, which is what the
// protobuf-inspecting predicate this replaced reported for a nil payload.
func (o Outcome) Type() Type {
	if o.typ == "" {
		return TypeUnknown
	}
	return o.typ
}

// Reason is Rejected's or Failed's account of itself, and empty for every other
// outcome.
func (o Outcome) Reason() string { return o.reason }

// Msg is the reply text Bounced or Errored was built from, and empty for every
// other outcome.
func (o Outcome) Msg() string { return o.msg }

// Permanent is meaningful on Bounced alone: see the constructor for what it
// does and does not assert.
func (o Outcome) Permanent() bool { return o.permanent }

// Code is the SMTP reply code Bounced or Errored carries. Failed has none by
// construction.
func (o Outcome) Code() uint32 { return o.code }

// UserAgent is retained on an engagement event under the Full Mode only.
func (o Outcome) UserAgent() string { return o.userAgent }

// IP is retained on an engagement event under the Full Mode only.
func (o Outcome) IP() string { return o.ip }

// URL is the link a Clicked event followed.
func (o Outcome) URL() string { return o.url }
