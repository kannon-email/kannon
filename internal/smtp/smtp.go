package smtp

// Sender interface represents a email sender
// object that can send a message
type Sender interface {
	Send(from string, to string, msg []byte) SenderError
	SenderName() string
}

// SenderOption configures a Sender built by NewSender.
type SenderOption func(*sender)

// AllowPrivateDestinations lets the Sender deliver to loopback, private,
// link-local and unspecified addresses, which it refuses by default.
func AllowPrivateDestinations() SenderOption {
	return func(s *sender) { s.allowPrivate = true }
}

// NewSender construct a new sender for a given hostname
func NewSender(hostname string, opts ...SenderOption) Sender {
	s := &sender{Hostname: hostname}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SenderError represent an smtp error
type SenderError interface {
	Error() string
	IsPermanent() bool
	Code() uint32
}
