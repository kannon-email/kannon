package fqdn

import (
	"fmt"
	"regexp"
)

type FQDN string

var ErrInvalidFQDN = fmt.Errorf("invalid FQDN")

// NewFQDN creates a new FQDN
func NewFQDN(fqdn string) (FQDN, error) {
	res := FQDN(fqdn)
	if !res.IsValid() {
		return "", fmt.Errorf("%w: %s", ErrInvalidFQDN, fqdn)
	}
	return res, nil
}

// String returns the string representation of the FQDN
func (f FQDN) String() string {
	return string(f)
}

// IsValid checks if the FQDN is valid using regex
var fqdnRegex = regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`)

// IsValid checks if the FQDN is valid
func (f FQDN) IsValid() bool {
	return fqdnRegex.MatchString(f.String())
}
