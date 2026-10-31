// Package apperr is the one shape an application error takes on its way to a transport. A Kind
// says what sort of refusal it is and a Code names it publicly; each transport maps the Kind to its
// own status once (ADR 0016), so no package restates the other packages' sentinels. Anything that
// is not an *Error is a fault, not a refusal.
package apperr

import "errors"

// Kind classifies a refusal independently of any transport.
type Kind int

const (
	// Malformed is a request that cannot be interpreted, such as an unparseable query or cursor.
	Malformed Kind = iota + 1
	// Invalid is a well-formed request that a rule refuses.
	Invalid
	// NotFound is a request naming something that does not exist, or that the caller may not see.
	NotFound
	// Conflict is a request that collides with existing state.
	Conflict
	// Forbidden is a request the caller is not permitted to make.
	Forbidden
)

// Error is an application refusal. Code is public and stable; the message and the cause are for
// logs and for legacy transports that already expose them.
type Error struct {
	Kind  Kind
	Code  string
	msg   string
	cause error
}

// New returns a refusal suitable as a sentinel; compare it with errors.Is.
func New(kind Kind, code, msg string) *Error {
	return &Error{Kind: kind, Code: code, msg: msg}
}

// Wrap classifies cause as a refusal while keeping its text as the message.
func Wrap(kind Kind, code string, cause error) *Error {
	return &Error{Kind: kind, Code: code, cause: cause}
}

func (e *Error) Error() string {
	switch {
	case e.msg != "":
		return e.msg
	case e.cause != nil:
		return e.cause.Error()
	default:
		return e.Code
	}
}

func (e *Error) Unwrap() error { return e.cause }

// As returns the outermost refusal in err's chain, if there is one.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
