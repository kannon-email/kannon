package audit

import (
	"context"
	"time"
)

// Repository is how an Audit Record is persisted — and only that. There is no read: nothing in
// Kannon queries this register, so no authorization decision can ever be influenced by what is in
// it, and a method that could be called would be an invitation to change that (ADR 0010).
//
// No service sits above it either. A Record is assembled at the producer and written at the
// consumer, and there is no logic in between to encapsulate.
type Repository interface {
	// Insert persists one Record. A Record whose identifier is already stored is a no-op rather
	// than an error: a message redelivered after a crash between the write and the acknowledgement
	// must not put one decision in the table twice, and the identifier is what makes that so.
	Insert(ctx context.Context, r Record) error

	// DeleteOlderThan removes every Record that occurred before the given instant, returning how
	// many. Idempotent, so replicas of the worker running it are harmless.
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
}
