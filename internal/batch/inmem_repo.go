package batch

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// InMemRepository is an in-memory Repository, for the tests of everything above it and for one half
// of the repository specification. Every Batch it hands out is a deep copy, so a caller mutating the
// attachments or headers it was given cannot change what is stored.
type InMemRepository struct {
	mu      sync.Mutex
	batches map[ID]*Batch
}

func NewInMemRepository() *InMemRepository {
	return &InMemRepository{batches: map[ID]*Batch{}}
}

func (r *InMemRepository) Create(_ context.Context, b *Batch) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.batches[b.ID()]; ok {
		return fmt.Errorf("batch %q already exists", b.ID())
	}
	r.batches[b.ID()] = copyBatch(b)
	return nil
}

func (r *InMemRepository) GetByID(_ context.Context, id ID) (*Batch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.batches[id]
	if !ok {
		return nil, ErrBatchNotFound
	}
	return copyBatch(b), nil
}

func copyBatch(b *Batch) *Batch {
	var attachments Attachments
	if b.Attachments() != nil {
		attachments = make(Attachments, len(b.Attachments()))
		for name, content := range b.Attachments() {
			attachments[name] = slices.Clone(content)
		}
	}
	return Load(LoadParams{
		ID:      b.ID(),
		Subject: b.Subject(),
		Sender:  b.Sender(),
		Domain:  b.Domain(),
		Headers: Headers{
			To: slices.Clone(b.Headers().To),
			Cc: slices.Clone(b.Headers().Cc),
		},
		TemplateID:          b.TemplateID(),
		Attachments:         attachments,
		OneClickUnsubscribe: b.OneClickUnsubscribe(),
		Tracking:            b.TrackingPolicy(),
	})
}
