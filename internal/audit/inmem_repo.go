package audit

import (
	"context"
	"slices"
	"sync"
	"time"
)

// InMemRepository is an in-memory Repository, for the tests of everything above it and for one half
// of the repository specification. Not a stub: what the sqlc-backed implementation has to do — a
// redelivered Record inserting nothing, a cutoff sparing the instant it names — this one has to do
// identically, or the specification would be a description of Postgres rather than of the contract.
type InMemRepository struct {
	mu      sync.Mutex
	records []Record
}

func NewInMemRepository() *InMemRepository {
	return &InMemRepository{}
}

// Insert stores a Record, ignoring one whose identifier is already held — the identifier alone, and
// none of the values beside it. Two identical decisions in the same instant are two operations and
// stay two rows, which is why the producer generates an identifier instead of the table deriving a
// natural key from the columns.
func (r *InMemRepository) Insert(_ context.Context, rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if slices.ContainsFunc(r.records, func(stored Record) bool { return stored.ID == rec.ID }) {
		return nil
	}

	r.records = append(r.records, clone(rec))
	return nil
}

// DeleteOlderThan drops every Record that occurred strictly before the cutoff, as the DELETE its
// counterpart runs does: a Record at the instant named survives, so two cleanup runs an hour apart
// cannot come to two answers about the Record sitting on their shared boundary.
func (r *InMemRepository) DeleteOlderThan(_ context.Context, before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := make([]Record, 0, len(r.records))
	var deleted int64
	for _, rec := range r.records {
		// Before and not a comparison on the values: a timestamptz read back may be in another
		// location, and the specification runs against both implementations.
		if rec.OccurredAt.Before(before) {
			deleted++
			continue
		}
		kept = append(kept, rec)
	}
	r.records = kept
	return deleted, nil
}

// Records is this implementation's Reader: everything stored under one Principal. It exists for the
// specification and for nothing else — Repository has no read on purpose (ADR 0010), and a method on
// the concrete type is a read the production code holding the interface cannot reach.
func (r *InMemRepository) Records(_ context.Context, principal string) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []Record
	for _, rec := range r.records {
		if rec.Principal == principal {
			found = append(found, clone(rec))
		}
	}
	return found, nil
}

// clone deep-copies a Record, on the way in and on the way back out. A Record is a value, but its
// Resource and its Grants are slices, so without this a caller would keep a handle on what the
// register says a decision was reached about — the same reason Resource.Segments returns a clone.
func clone(rec Record) Record {
	cp := rec
	cp.Resource = slices.Clone(rec.Resource)
	cp.Details.Grants = slices.Clone(rec.Details.Grants)
	return cp
}
