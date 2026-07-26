package envelope

import (
	"sync"

	"github.com/kannon-email/kannon/internal/tracking"
)

// sharedTokenGeneration is how many tokens one generation of the cache holds
// before it is rotated; the cache therefore never holds more than twice this
// many entries. A Batch occupies one entry for its pixel plus one per distinct
// tracked URL, so a few dozen entries per Batch is typical and this leaves room
// for the handful of Batches a Dispatcher has in flight at once. At roughly a
// kilobyte per RS512 token that is a low-single-digit-megabyte ceiling.
const sharedTokenGeneration = 1024

// sharedTokenKey names a token that may be reused. It carries everything that
// must never be shared across: the axis (an open token and a link token are
// different claim shapes), the Batch, the Domain, the tracked URL, and the
// Tracking Mode. Domain is strictly redundant — a Batch belongs to exactly one
// Domain, and its id spells the Domain out — but naming it keeps the guarantee
// legible here rather than resting on the format of an id parsed elsewhere.
type sharedTokenKey struct {
	axis      tracking.Axis
	domain    string
	messageID string
	url       string
	mode      tracking.Mode
}

// sharedTokens reuses the tokens that carry no Recipient identity, so a Batch
// signs one open token and one link token per URL instead of one of each per
// Delivery. On a Batch of 100k Deliveries with ten links that is eleven RSA-4096
// signatures instead of roughly 1.1M.
//
// Reuse — rather than minting equal claims twice — is what the Anonymous property
// actually requires: a JWT carries iat and exp, so two independent mints of
// identical claims are not the same token, and two Recipients holding two
// distinguishable tokens can be told apart even if neither names them.
//
// The Dispatcher builds across many Batches over a process lifetime, so the cache
// is bounded. It is bounded by rotating whole generations rather than by tracking
// per-entry recency: the access pattern is a Dispatcher working through the
// Deliveries of a Batch, which is strongly clustered in time, so an entry that
// falls out of the live generation is either finished with or promoted back on its
// next use. Two generations, rather than clearing outright, are what keep a
// working set slightly larger than one generation from thrashing.
//
// Scoping the cache to a single dispatch pass was the alternative and would need
// no bound at all, but a pass claims at most a page of Deliveries — so a 100k
// Batch would still sign thousands of times, and the property this exists for
// would be lost.
type sharedTokens struct {
	mu   sync.Mutex
	live map[sharedTokenKey]string
	prev map[sharedTokenKey]string
}

func newSharedTokens() *sharedTokens {
	return &sharedTokens{
		live: make(map[sharedTokenKey]string, sharedTokenGeneration),
	}
}

// reuse returns the token held for key, calling mint once if neither generation
// holds one. A failed mint is not cached.
//
// mint runs while the lock is held. That is deliberate: two Builds racing on the
// same missing key would otherwise each mint, and their two Deliveries would go
// out with two different tokens — precisely the property Anonymous exists to
// provide. The cost is confined to this path, since a Mode that identifies the
// Recipient never consults the cache at all, and a miss happens about once per
// Batch.
func (c *sharedTokens) reuse(key sharedTokenKey, mint func() (string, error)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if token, ok := c.live[key]; ok {
		return token, nil
	}
	if token, ok := c.prev[key]; ok {
		c.put(key, token)
		return token, nil
	}

	token, err := mint()
	if err != nil {
		return "", err
	}
	c.put(key, token)
	return token, nil
}

// put stores a token in the live generation, rotating first if that generation is
// full. Callers hold the lock.
func (c *sharedTokens) put(key sharedTokenKey, token string) {
	if len(c.live) >= sharedTokenGeneration {
		c.prev, c.live = c.live, make(map[sharedTokenKey]string, sharedTokenGeneration)
	}
	c.live[key] = token
}

// len reports how many entries the cache holds across both generations, for the
// test that pins the memory bound.
func (c *sharedTokens) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.live) + len(c.prev)
}
