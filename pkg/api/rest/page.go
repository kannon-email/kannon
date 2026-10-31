package rest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/kannon-email/kannon/internal/keyset"
)

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

// pageRequest is one list request: the requested size plus the position to resume after.
// The cursor binds a scope naming the collection and every filter that shapes it, so a cursor
// replayed against another Domain, collection or query is refused instead of silently skipping.
type pageRequest[K any] struct {
	scope string
	after *K
	limit int
}

// cursor is the opaque wire form: base64url(JSON). The scope is hashed so filters stay unreadable
// and the token stays short however many filters a collection has.
type cursor[K any] struct {
	Scope string `json:"s"`
	Key   K      `json:"k"`
}

// readPage parses limit and cursor. scope names the collection, then every value that narrows it
// (Domain, parent IDs, filters). valid rejects decoded keys no row could have produced.
func readPage[K any](q url.Values, valid func(K) bool, scope ...any) (pageRequest[K], error) {
	digest, err := scopeDigest(scope)
	if err != nil {
		return pageRequest[K]{}, err
	}
	p := pageRequest[K]{scope: digest, limit: defaultPageLimit}
	if q.Has(limitParameter) {
		n, err := strconv.Atoi(q.Get(limitParameter))
		if err != nil || n < 1 || n > maxPageLimit {
			return p, problemError(400, "invalid_query_parameter")
		}
		p.limit = n
	}
	if !q.Has(cursorParameter) {
		return p, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(q.Get(cursorParameter))
	var c cursor[K]
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Scope != p.scope || !valid(c.Key) {
		return p, problemError(400, "invalid_cursor")
	}
	p.after = &c.Key
	return p, nil
}

// keyset asks the operation for one row beyond the page, which is how writePage learns another follows.
func (p pageRequest[K]) keyset() keyset.Page[K] {
	return keyset.Page[K]{After: p.after, Limit: p.limit + 1}
}

// writePage trims the lookahead row and, when it was present, encodes the last kept row's position.
func writePage[T, K any](p pageRequest[K], rows []T, key func(T) K) ([]T, *string, error) {
	if len(rows) <= p.limit {
		return rows, nil, nil
	}
	rows = rows[:p.limit]
	raw, err := json.Marshal(cursor[K]{Scope: p.scope, Key: key(rows[len(rows)-1])})
	if err != nil {
		return nil, nil, err
	}
	next := base64.RawURLEncoding.EncodeToString(raw)
	return rows, &next, nil
}

// scopeDigest hashes the JSON form of the scope, so no two distinct scopes can collide by concatenation.
func scopeDigest(scope []any) (string, error) {
	raw, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(sum[:16]), nil
}
