// Package requestid names a request in the responses and the logs of every API transport, so a
// caller can quote the ID and an operator can find the log line it belongs to.
package requestid

import (
	"crypto/rand"
	"encoding/base64"
)

// Header carries the request ID: a REST response header and Connect error metadata.
const Header = "X-Request-ID"

// New returns a fresh random request ID.
func New() string {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return base64.RawURLEncoding.EncodeToString(id)
}
