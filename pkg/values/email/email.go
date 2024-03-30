package email

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrInvalidEmail = errors.New("invalid email")

type Email string

func NewEmail(v string) (Email, error) {
	if !isValidEmail(v) {
		return "", fmt.Errorf("%w: %s", ErrInvalidEmail, v)
	}
	return Email(v), nil
}

func NewEmailFromPtr(v *string) (*Email, error) {
	if v == nil {
		return nil, nil
	}

	email, err := NewEmail(*v)
	if err != nil {
		return nil, err
	}

	return &email, nil
}

func (e Email) String() string {
	return string(e)
}

func (e Email) Username() string {
	return strings.Split(e.String(), "@")[0]
}

func (e Email) Domain() string {
	return strings.Split(e.String(), "@")[1]
}

var reg = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func isValidEmail(email string) bool {
	return reg.MatchString(email)
}

func SafeToString(e *Email) string {
	if e == nil {
		return ""
	}

	return e.String()
}

func SafeToStringPtr(e *Email) *string {
	if e == nil {
		return nil
	}

	v := e.String()
	return &v
}
