package smtp

import (
	"errors"
	"net"
	"regexp"
	"strings"
)

var emailValidator = regexp.MustCompile(`^[^@ \t\r\n]+@[^@ \t\r\n]+\.[^@ \t\r\n]{2,}$`)

// Validate reports whether addr is a formally correct address on a domain Kannon
// may deliver to.
func Validate(addr string) bool {
	if !emailValidator.MatchString(addr) {
		return false
	}
	return !namesHostDirectly(addr[strings.LastIndex(addr, "@")+1:])
}

// namesHostDirectly reports whether a domain names a host instead of a mail
// domain: an address literal, a bare IP, or localhost. Mail to such a domain
// has no MX to resolve, so the SMTPSender would dial the host itself, which
// lets a tenant aim a Delivery at an internal service.
//
// A top-level label made only of digits is never a real TLD (RFC 3696 §2), and
// rejecting it also catches the shortened IPv4 forms such as 127.1.
func namesHostDirectly(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if strings.ContainsAny(domain, "[]:") || net.ParseIP(domain) != nil {
		return true
	}
	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		return true
	}
	tld := domain[strings.LastIndex(domain, ".")+1:]
	return strings.Trim(tld, "0123456789") == ""
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
