package tracking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// The identity a tracking token carries is always email-shaped, and which address it is is decided
// by the Tracking Mode (ADR 0006): the Recipient's own, or a sentinel address under the Modes that
// name nobody. Sentinels live under a reserved subdomain, since anonymous@<domain> can be real.
const (
	// reservedLabel is the subdomain reserved for sentinel addresses. Operators
	// must not deliver mail under it — see ARCHITECTURE.md.
	reservedLabel = "track"
	// anonymousLocalPart names the sentinel that stands for no one at all. It is
	// constant per Domain by design; see AnonymousIdentity.
	anonymousLocalPart = "anonymous"
	// pseudonymBytes is the entropy behind one pseudonym: 128 bits, far past any
	// birthday collision within the Batch a pseudonym is meaningful in.
	pseudonymBytes = 16
)

// ReservedNamespace returns the host part every sentinel address of domain lives
// under.
func ReservedNamespace(domain string) string {
	return reservedLabel + "." + domain
}

// AnonymousIdentity returns the sentinel an Anonymous token carries for domain. It exists for
// uniformity of the claim type: being constant per Domain is the point, so one Batch needs one
// token and one RSA-4096 signature rather than one per Delivery.
func AnonymousIdentity(domain string) string {
	return anonymousLocalPart + "@" + ReservedNamespace(domain)
}

// NewPseudonym returns a fresh Pseudonymous identity under domain: 128 bits from crypto/rand as
// lowercase hex, since email pipelines case-fold. Drawn at random and derived from nothing, so
// nobody — Kannon included — can link two of them (ADR 0006). One per Delivery is the rung.
func NewPseudonym(domain string) (string, error) {
	raw := make([]byte, pseudonymBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("cannot generate tracking pseudonym: %w", err)
	}
	return hex.EncodeToString(raw) + "@" + ReservedNamespace(domain), nil
}

// InReservedNamespace reports whether identity is a sentinel address of domain: a non-empty local
// part directly under the reserved namespace. The host is compared case-insensitively; the local
// part is not inspected, since the question is which namespace it sits in, not how it was drawn.
func InReservedNamespace(identity, domain string) bool {
	local, host, ok := strings.Cut(identity, "@")
	return ok && local != "" && strings.EqualFold(host, ReservedNamespace(domain))
}

// IsPseudonym reports whether identity may be minted as a Pseudonymous identity of domain: a
// sentinel address that is not the one standing for no one. Excluding the Anonymous sentinel keeps
// the mint in step with the Stats worker, which would drop such an event as an upstream bug.
func IsPseudonym(identity, domain string) bool {
	return InReservedNamespace(identity, domain) && !NamesNobody(identity, domain)
}

// NamesNobody reports whether identity attributes an event to no one: the Anonymous sentinel of
// domain, or nothing at all, which is what a pre-upgrade token carries. A pseudonym is not one of
// these — it names nobody to a person, but it does name one Delivery, which is the rung.
func NamesNobody(identity, domain string) bool {
	return identity == "" || strings.EqualFold(identity, AnonymousIdentity(domain))
}
