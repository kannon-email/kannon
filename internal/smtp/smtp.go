package smtp

// Sender interface represents a email sender
// object that can send a message
type Sender interface {
	Send(from string, to string, msg []byte) SenderError
	SenderName() string
}

// NewSender construct a new sender for a given hostname
func NewSender(hostname string) Sender {
	return &sender{
		Hostname: hostname,
	}
}

// SenderError represent an smtp error
type SenderError interface {
	Error() string
	IsPermanent() bool
	Code() int
}
