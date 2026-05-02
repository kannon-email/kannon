package batch

import "context"

// Repository persists Batch entities.
type Repository interface {
	// Create persists a new Batch. The ID must already be populated by New.
	Create(ctx context.Context, b *Batch) error

	// GetByID looks up a Batch by its ID.
	// Returns ErrBatchNotFound if not present.
	GetByID(ctx context.Context, id ID) (*Batch, error)
}
