package domains

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

// initialPolicy is the Tracking Policy a Domain starts with. The sqlc-backed Repository gets it from
// the column default; this one states it, and RunRepoSpec holds both to the same value.
var initialPolicy = tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified}

// InMemRepository is an in-memory Repository, for the tests of everything above it and for one half
// of the repository specification. Every entity it hands out is a fresh copy, so a caller mutating
// what it was given cannot change what is stored — as with a row read back from Postgres.
type InMemRepository struct {
	mu     sync.Mutex
	nextID int32
	byName map[values.DomainName]*Domain
}

func NewInMemRepository() *InMemRepository {
	return &InMemRepository{byName: map[values.DomainName]*Domain{}}
}

func (r *InMemRepository) Create(_ context.Context, d *Domain) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byName[d.Name()]; ok {
		return ErrDomainAlreadyExists
	}
	r.nextID++
	stored := Load(LoadParams{
		ID:             r.nextID,
		Domain:         d.Name(),
		DkimPrivateKey: d.DkimPrivateKey(),
		DkimPublicKey:  d.DkimPublicKey(),
		CreatedAt:      time.Now(),
		Tracking:       initialPolicy,
	})
	r.byName[d.Name()] = stored
	*d = *copyDomain(stored)
	return nil
}

func (r *InMemRepository) SetTrackingPolicy(_ context.Context, domain values.DomainName, p tracking.Policy) (*Domain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.byName[domain]
	if !ok {
		return nil, ErrDomainNotFound
	}
	d.tracking = p.Normalized()
	return copyDomain(d), nil
}

func (r *InMemRepository) FindByName(_ context.Context, domain values.DomainName) (*Domain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.byName[domain]
	if !ok {
		return nil, ErrDomainNotFound
	}
	return copyDomain(d), nil
}

// List returns every Domain in creation order, as the sqlc-backed Repository orders by its serial id.
func (r *InMemRepository) List(_ context.Context) ([]*Domain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.sorted(func(a, b *Domain) int { return cmp.Compare(a.ID(), b.ID()) }), nil
}

func (r *InMemRepository) ListPage(_ context.Context, page keyset.Page[values.DomainName]) ([]*Domain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := []*Domain{}
	for _, d := range r.sorted(func(a, b *Domain) int { return strings.Compare(a.Domain(), b.Domain()) }) {
		if page.After != nil && d.Domain() <= page.After.String() {
			continue
		}
		if len(out) == page.Limit {
			break
		}
		out = append(out, d)
	}
	return out, nil
}

// sorted returns copies of every Domain in the given order. The caller holds the lock.
func (r *InMemRepository) sorted(order func(a, b *Domain) int) []*Domain {
	out := make([]*Domain, 0, len(r.byName))
	for _, d := range r.byName {
		out = append(out, copyDomain(d))
	}
	slices.SortFunc(out, order)
	return out
}

// copyDomain detaches a Domain from the stored one. The Tracking Policy is normalised on the way
// out, as the sqlc-backed Repository does, so a Domain always states a ceiling on both axes.
func copyDomain(d *Domain) *Domain {
	return Load(LoadParams{
		ID:             d.ID(),
		Domain:         d.Name(),
		DkimPrivateKey: d.DkimPrivateKey(),
		DkimPublicKey:  d.DkimPublicKey(),
		CreatedAt:      d.CreatedAt(),
		Tracking:       d.TrackingPolicy().Normalized(),
	})
}
