package tracking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// The identity a tracking token carries is always email-shaped, and which
// address it is, is decided by the Tracking Mode (ADR 0006): the Recipient's own
// under a Mode that names them, and a **sentinel address** under the Modes that
// name nobody. Keeping the claim one type is what lets the Stats worker, the
// `stats` schema and the API surface stay untouched by the Modes that identify
// no one — a Pseudonymous event flows through the same per-recipient write path
// as an Identified one, and the Mode is the discriminator.
//
// The sentinels live under a reserved subdomain of the sending Domain rather
// than at the bare Domain, because `anonymous@<fqdn>` can be a real mailbox: the
// reservation exists so the sentinel space and the deliverable space cannot
// collide.
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

// ReservedNamespace returns the domain part every sentinel address of fqdn lives
// under.
func ReservedNamespace(fqdn string) string {
	return reservedLabel + "." + fqdn
}

// AnonymousIdentity returns the sentinel an Anonymous token carries for fqdn.
//
// It exists for uniformity of the claim type and nothing else: an Anonymous event
// is counted in aggregate only and never reaches the `stats` table. Being constant
// per Domain is the point — the token an Anonymous axis mints names nobody and is
// therefore the same token for every Recipient of a Batch, one RSA-4096 signature
// instead of one per Delivery.
func AnonymousIdentity(fqdn string) string {
	return anonymousLocalPart + "@" + ReservedNamespace(fqdn)
}

// NewPseudonym returns a fresh Pseudonymous identity under fqdn: 128 bits from
// crypto/rand as a lowercase hex local part.
//
// Lowercase hex because email pipelines case-fold, and a case-sensitive alphabet
// would let a lowercasing in transit merge two pseudonyms into one.
//
// It is drawn at random, stored nowhere and derived from nothing. That is what the
// rung promises: the same Recipient in two Batches carries two pseudonyms nobody —
// Kannon included — can link, and no key holder can confirm a guessed (address,
// pseudonym) pairing, which a deterministic derivation such as an HMAC of the
// address would allow (ADR 0006).
//
// The caller decides how often to call it, and that decision *is* the rung: one
// pseudonym per Delivery makes a Delivery's events linkable to each other and to
// nothing else. See internal/envelope.
func NewPseudonym(fqdn string) (string, error) {
	raw := make([]byte, pseudonymBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("cannot generate tracking pseudonym: %w", err)
	}
	return hex.EncodeToString(raw) + "@" + ReservedNamespace(fqdn), nil
}

// InReservedNamespace reports whether identity is a sentinel address of fqdn: a
// non-empty local part directly under the reserved namespace.
//
// It is the machine-checkable form of the reservation. The domain part is
// compared case-insensitively for the same reason pseudonyms are lowercase hex;
// the local part is not inspected, since the question is which namespace an
// identity sits in and not how it was drawn.
func InReservedNamespace(identity, fqdn string) bool {
	local, host, ok := strings.Cut(identity, "@")
	return ok && local != "" && strings.EqualFold(host, ReservedNamespace(fqdn))
}

// IsPseudonym reports whether identity may be minted as a Pseudonymous identity
// of fqdn: a sentinel address that is not the one standing for no one.
//
// The mint uses it to refuse a token claiming to be pseudonymous while carrying
// a real address. Excluding the Anonymous sentinel is what keeps that refusal in
// step with the Stats worker, which reads the same address as naming nobody and
// would refuse to record it: without this, the two chokepoints would disagree
// about the one address they both have an opinion on, and a Pseudonymous event
// could be minted only to be dropped downstream as a bug.
func IsPseudonym(identity, fqdn string) bool {
	return InReservedNamespace(identity, fqdn) && !NamesNobody(identity, fqdn)
}

// NamesNobody reports whether identity attributes an event to no one: either the
// Anonymous sentinel of fqdn, or nothing at all.
//
// The empty case is a token minted before the identity claim was always an
// address, which keeps arriving for one token lifetime after this build ships.
// A pseudonym is deliberately *not* one of these — it names nobody in the sense
// that matters to a person, but it does name one Delivery, which is exactly what
// the Pseudonymous rung records.
func NamesNobody(identity, fqdn string) bool {
	return identity == "" || strings.EqualFold(identity, AnonymousIdentity(fqdn))
}
