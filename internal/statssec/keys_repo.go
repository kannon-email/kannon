package statssec

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNoSigningKey is returned when no stored key stays valid long enough to sign with.
	ErrNoSigningKey = errors.New("no signing key valid for long enough")
	// ErrKeyNotFound is returned when a token names a key that is unknown or already expired.
	ErrKeyNotFound = errors.New("tracking key not found or expired")
)

// Key is one RSA key pair tracking tokens are signed with, PEM-encoded, and the instant after
// which a token naming it no longer verifies.
type Key struct {
	ID         string
	PrivateKey string
	PublicKey  string
	ExpiresAt  time.Time
}

// KeyRepository stores the key pairs tracking tokens are signed and verified with. A key is
// minted on demand, used until it is too close to expiry to outlive the tokens it would sign, and
// swept once it can no longer verify anything.
type KeyRepository interface {
	// SigningKey returns a key that is still valid at validUntil, so a token signed with it can
	// be verified for as long as the token lives. Any such key will do. Returns ErrNoSigningKey
	// when none is.
	SigningKey(ctx context.Context, validUntil time.Time) (Key, error)

	// Create stores a new key.
	Create(ctx context.Context, k Key) error

	// PublicKey returns the public half of the key with the given ID, as long as the key has not
	// expired. Returns ErrKeyNotFound for an unknown or expired key.
	PublicKey(ctx context.Context, id string) (string, error)

	// DeleteExpired removes every expired key and returns how many it removed.
	DeleteExpired(ctx context.Context) (int64, error)
}
