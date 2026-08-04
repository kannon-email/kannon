package authz

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxAttributionLength caps a claim in bytes. An Attribution names one person, and RFC 5321
// already caps an address at 256, so nothing honest is refused — while a header of unbounded
// length cannot become a log line of unbounded length on every operation it accompanies.
const maxAttributionLength = 256

// Attribution is a claim naming who asked, on the far side of a caller Kannon cannot see
// into. Unverifiable in principle, so it is recorded and never consulted — reaching an
// authorization decision would let a holder choose its own authority. It is personal data.
type Attribution string

// ParseAttribution is where a claim arriving from a caller becomes a typed one. It validates the
// shape of the string and nothing about the person: there is nobody to check against, which is
// the whole of ADR 0008. What it does check is what a record can survive — a claim is written
// down, so a malformed claim is a malformed record, made at the one point that still knows the
// request it came from. Surrounding whitespace is trimmed for the same reason a token's is: a
// value copied into a header or a config field arrives padded often enough that treating the
// padding as part of the name would record two spellings of one person.
func ParseAttribution(s string) (Attribution, error) {
	claim := strings.TrimSpace(s)

	if claim == "" {
		return "", errors.New("attribution names nobody")
	}
	if len(claim) > maxAttributionLength {
		return "", fmt.Errorf("attribution is %d bytes, over the %d-byte limit", len(claim), maxAttributionLength)
	}
	// Invalid UTF-8 is refused here rather than at the far end of the request: a header carries
	// arbitrary bytes, and Postgres rejects them in a text column, so a claim that cannot be
	// stored would surface as an operation failing for a reason no caller could read.
	if !utf8.ValidString(claim) {
		return "", errors.New("attribution is not valid UTF-8")
	}
	// A control character in a claim is either a caller's bug or an attempt to forge the
	// structure of the record it is about to appear in. Neither is part of anybody's name.
	for _, r := range claim {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("attribution contains the control character %q", r)
		}
	}

	return Attribution(claim), nil
}

// String returns the claim as written.
func (a Attribution) String() string {
	return string(a)
}
