// Package envelope defines the Envelope domain entity per CONTEXT.md: the
// fully-built per-recipient outgoing email — body, headers, identifiers —
// that the SMTPSender transmits. The translation to the proto EmailToSend
// type lives in internal/envelopepb, so this package stays free of any
// protobuf dependency.
package envelope

// Envelope is the per-recipient outgoing email: the signed RFC 2822 body
// plus the addressing metadata needed by the SMTPSender.
type Envelope struct {
	emailID     string
	from        string
	to          string
	returnPath  string
	body        []byte
	shouldRetry bool
}

// Params groups the fields needed to construct an Envelope.
type Params struct {
	EmailID     string
	From        string
	To          string
	ReturnPath  string
	Body        []byte
	ShouldRetry bool
}

// New builds an Envelope from the given fields.
func New(p Params) *Envelope {
	return &Envelope{
		emailID:     p.EmailID,
		from:        p.From,
		to:          p.To,
		returnPath:  p.ReturnPath,
		body:        p.Body,
		shouldRetry: p.ShouldRetry,
	}
}

// Getters

func (e *Envelope) EmailID() string    { return e.emailID }
func (e *Envelope) From() string       { return e.from }
func (e *Envelope) To() string         { return e.to }
func (e *Envelope) ReturnPath() string { return e.returnPath }
func (e *Envelope) Body() []byte       { return e.body }
func (e *Envelope) ShouldRetry() bool  { return e.shouldRetry }
