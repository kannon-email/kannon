package envelope

import (
	"sync"

	"github.com/kannon-email/kannon/internal/tracking"
)

// sharedTokenGeneration is how many tokens one generation holds before it is
// rotated, so the cache never holds more than twice this many — a few megabytes
// at roughly a kilobyte per RS512 token. A Batch occupies one entry for its pixel
// plus one per distinct tracked URL.
const sharedTokenGeneration = 1024

// sharedTokenKey names a token that may be reused, and must carry everything the
// token commits to: sharing across any of these would hand one Recipient's token
// to another. Domain is redundant — a Batch id spells its Domain out — but naming
// it keeps that guarantee here rather than in the format of an id parsed
// elsewhere.
type sharedTokenKey struct {
	axis      tracking.Axis
	domain    string
	messageID string
	url       string
	mode      tracking.Mode
}

// sharedTokens reuses the tokens that carry no Recipient identity, so a Batch
// signs one open token and one link token per URL instead of one of each per
// Delivery — eleven RSA-4096 signatures instead of roughly 1.1M on a 100k Batch
// with ten links.
//
// Reuse rather than minting equal claims twice is what the Anonymous property
// requires: a JWT carries iat and exp, so two independent mints are not the same
// token, and two Recipients holding two distinguishable tokens can be told apart
// even if neither names them.
//
// The Dispatcher builds across many Batches over a process lifetime, so the cache
// is bounded by rotating whole generations rather than by tracking per-entry
// recency. Its access pattern is one Batch's Deliveries in succession, strongly
// clustered in time, so an entry falling out of the live generation is either
// finished with or promoted back on its next use; the second generation keeps a
// working set slightly larger than one generation from thrashing.
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
// mint runs while the lock is held, deliberately: two Builds racing on the same
// missing key would otherwise each mint, and their two Deliveries would go out
// with two distinguishable tokens. The cost is confined to a miss — about one per
// Batch — since a Mode that identifies the Recipient never consults the cache.
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
