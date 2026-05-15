package smtp

import (
	"errors"
	"regexp"
	"strings"
)

var emailValidator = regexp.MustCompile(`^[^@ \t\r\n]+@[^@ \t\r\n]+\.[^@ \t\r\n]{2,}$`)

// Validate if email address is formally correct
func Validate(addr string) bool {
	return emailValidator.MatchString(addr)
}

// GetEmailDomain extracts domain host from a given email address
func GetEmailDomain(addr string) (string, error) {
	_, domain, err := SplitEmail(addr)
	return domain, err
}

// SplitEmail extracts name and domain host from email
func SplitEmail(addr string) (local, domain string, err error) {
	if !Validate(addr) {
		return "", "", errors.New("mta: invalid mail address")
	}
	parts := strings.SplitN(addr, "@", 2)

	if len(parts) != 2 {
		// Should never be called!
		return "", "", errors.New("mta: invalid mail address")
	}
	return parts[0], parts[1], nil
}
