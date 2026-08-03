// Package values holds small, immutable value types validated at construction.
package values

import (
	"errors"
	"fmt"
	"strings"
)

// maxLength matches domains.domain, the narrowest column a domain name lands in, so nothing this
// package accepts can fail to store. The DNS presentation limit is 253; the extra byte is
// tolerated rather than turned into a new way for existing data to be rejected.
const maxLength = 254

// DomainName is a Domain's domain name in canonical form: lower-cased, without "/", "*" or "@",
// with at least one dot and no trailing one, so a name can neither alias a segment of the
// Resource tree nor spell one Domain twice (ADR 0008). Comparable, so it works as a map key and
// with ==, and two DomainNames are equal exactly when they name the same Domain — the property
// the whole authority model rests on. Only Parse builds one.
type DomainName struct {
	// Unexported so that a value can only originate from Parse. A defined
	// string type would allow DomainName("TEST.com") by conversion, which is
	// the one thing this type exists to prevent.
	s string
}

// Parse canonicalises (lower-cases) and validates a domain name. It rejects what would corrupt a
// Resource path or an identifier embedding a name, and rejects a single-label name, which could
// equal a segment of the tree; every real mail domain carries a dot, so nothing real is lost.
func Parse(s string) (DomainName, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	if s == "" {
		return DomainName{}, errors.New("domain name is required")
	}
	if len(s) > maxLength {
		return DomainName{}, fmt.Errorf("domain name must be %d characters or less", maxLength)
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return DomainName{}, errors.New("domain name must not start or end with a dot")
	}
	if strings.Contains(s, "..") {
		return DomainName{}, errors.New("domain name must not contain an empty label")
	}
	if !strings.Contains(s, ".") {
		return DomainName{}, errors.New("domain name must contain at least one dot")
	}
	for _, r := range s {
		if !isAllowed(r) {
			return DomainName{}, fmt.Errorf("domain name contains a disallowed character %q", r)
		}
	}

	return DomainName{s: s}, nil
}

// MustParse is Parse for package-level values and tests, where a bad domain
// name is a programming error rather than input.
func MustParse(s string) DomainName {
	n, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String returns the canonical domain name. The zero value returns "".
func (n DomainName) String() string {
	return n.s
}

// IsZero reports whether this DomainName names no Domain.
func (n DomainName) IsZero() bool {
	return n.s == ""
}

// isAllowed reports whether r may appear in a domain name. An allowlist rather than a denylist of
// hazards: "/", "*" and "@" are the dangerous ones, but saying what is permitted also excludes
// whitespace, control characters and homoglyphs. "_" is permitted, as real mail names use it.
func isAllowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '-' || r == '.' || r == '_':
		return true
	default:
		return false
	}
}
