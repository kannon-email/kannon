// Package returnpath is the bounce address Kannon puts on every Envelope, and the only thing that
// can read one back.
//
// A remote MTA that gives up on a Delivery reports it to the envelope sender, so the return path is
// how an asynchronous DSN finds its way to the Delivery it is about: it names the Recipient and the
// Batch, and the inbound SMTP server turns what it names into a Bounced event — one the Dispatcher
// acts on by dropping the Delivery, and the stats worker records as a bounce.
//
// Everything a return path names is known to whoever received the message: the Batch ID travels in
// its headers and the address is just base64. Unsigned, it let anyone who could reach the inbound
// SMTP server cancel any Recipient's queued or retrying Delivery, and record a bounce against any
// address of any Domain. So the return path carries an HMAC over what it names, keyed by a secret
// only the processes that build and read return paths hold, and one that does not verify is not a
// bounce Kannon will report.
package returnpath

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kannon-email/kannon/internal/utils"
)

// MinSecretLength is the shortest secret Parse accepts. The secret is the whole of what stands
// between a forged DSN and a dropped Delivery, and it is never typed by a person, so there is no
// reason to accept one short enough to guess.
const MinSecretLength = 32

// macLength is how many bytes of the HMAC-SHA256 the address carries: 128 bits is far beyond what
// an attacker can search through an SMTP server, and every byte of it is spent out of a local part
// that RFC 5321 would rather keep under 64 octets.
const macLength = 16

// domainSeparator keeps this MAC from ever being mistaken for one computed over the same secret for
// some other purpose, and fixes the version of the layout it covers.
const domainSeparator = "kannon-return-path-v1"

// ErrInvalidSignature is what Parse answers for a return path that has the shape of a bounce
// address but was not signed with this secret — a forgery, a typo, or one built before return
// paths were signed. The three are one error because the answer to all of them is the same: this
// is not a bounce to report.
var ErrInvalidSignature = errors.New("return path signature does not verify")

// The "bump_" prefix is the wire-format token the inbound SMTP server recognises; renaming it would
// be wire-breaking. The layout is bump_<b64url(email)>.<b64url(mac)>+<messageID>: '+' separates the
// fields, which is why the URL-safe alphabet (which never emits it) encodes both segments, and '.'
// is outside that alphabet too, so it can separate the two without escaping.
var parseReturnPath = regexp.MustCompile(`bump_([^+]*)\+(.*)`)

// Signer builds and verifies return paths with one secret. The zero Signer holds no secret, so it
// verifies nothing and refuses to build: a process that was never given the secret fails closed.
type Signer struct {
	key []byte
}

// Parse resolves a configured secret into a Signer. Surrounding whitespace is trimmed, since a
// secret mounted from a Kubernetes Secret arrives with a trailing newline often enough that keeping
// it would leave the builder and the reader disagreeing about the key.
func Parse(secret string) (Signer, error) {
	s := strings.TrimSpace(secret)
	if len(s) < MinSecretLength {
		return Signer{}, fmt.Errorf("bounce secret must be at least %d characters long", MinSecretLength)
	}
	return Signer{key: []byte(s)}, nil
}

// MustParse is Parse for tests and package-level values, where a short secret is a programming
// error rather than an operator's.
func MustParse(secret string) Signer {
	s, err := Parse(secret)
	if err != nil {
		panic(err)
	}
	return s
}

// Build returns the return path for one Recipient of one Batch.
func (s Signer) Build(email, messageID string) string {
	if len(s.key) == 0 {
		// Unreachable through Parse. Panicking rather than signing with an empty key, which would
		// put a return path anyone can forge on every message the process sends.
		panic("returnpath: Build called on a zero Signer")
	}
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(email))
	mac := base64.RawURLEncoding.EncodeToString(s.mac(email, messageID))
	return fmt.Sprintf("bump_%v.%v+%v", emailBase64, mac, messageID)
}

// Parse reads a return path back into the Recipient and Batch it names. found reports whether the
// address is a bounce return path at all; an address that is one but does not verify is found,
// with ErrInvalidSignature, so the caller can tell a forgery from mail that was never for it.
func (s Signer) Parse(returnPath string) (email, messageID, domain string, found bool, err error) {
	match := parseReturnPath.FindStringSubmatch(returnPath)
	if match == nil {
		return "", "", "", false, nil
	}
	encoded, messageID := match[1], match[2]

	emailSegment, macSegment, ok := cutLast(encoded, ".")
	if !ok || len(s.key) == 0 {
		return "", "", "", true, ErrInvalidSignature
	}

	gotMAC, err := base64.RawURLEncoding.DecodeString(macSegment)
	if err != nil {
		return "", "", "", true, ErrInvalidSignature
	}

	// The email segment is encoded with the URL-safe alphabet by Build; the standard one can emit
	// '+', the field separator, so it is not a valid choice for this segment (#432).
	emailBytes, err := base64.URLEncoding.DecodeString(emailSegment)
	if err != nil {
		return "", "", "", true, ErrInvalidSignature
	}
	email = string(emailBytes)

	if !hmac.Equal(gotMAC, s.mac(email, messageID)) {
		return "", "", "", true, ErrInvalidSignature
	}

	domain, err = utils.ExtractDomainFromMessageID(messageID)
	if err != nil {
		return "", "", "", true, err
	}
	return email, messageID, domain, true, nil
}

// mac covers both fields, each prefixed with its length so that no split of the same bytes into a
// different Recipient and Batch hashes alike: a signature over one Recipient of one Batch vouches
// for nothing else.
func (s Signer) mac(email, messageID string) []byte {
	h := hmac.New(sha256.New, s.key)
	for _, field := range []string{domainSeparator, email, messageID} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(field)))
		h.Write(n[:])
		h.Write([]byte(field))
	}
	return h.Sum(nil)[:macLength]
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
