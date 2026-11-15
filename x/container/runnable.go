package container

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// Runnable describes one component the application starts. Name is used for
// logging and diagnostics; Run is the long-running entry point and must return
// when ctx is cancelled.
//
// Run must call ready once the component can do its job: its listeners are
// bound and the NATS streams and consumers it relies on exist. A component that
// returns without calling it keeps the whole process from being ready.
type Runnable struct {
	Name string
	Run  func(ctx context.Context, ready func()) error
}

// Registry collects Runnables and starts them together under a shared context.
type Registry struct {
	runnables []Runnable
}

// Register adds a Runnable to the registry. Order of registration is preserved.
func (r *Registry) Register(run Runnable) {
	r.runnables = append(r.runnables, run)
}

// Names returns the names of all registered runnables in registration order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.runnables))
	for i, run := range r.runnables {
		names[i] = run.Name
	}
	return names
}

// Run starts every registered runnable in its own goroutine under a shared
// errgroup-derived context, propagates ctx cancellation, and returns the first
// non-nil error. onReady is called once, when every runnable has called ready.
func (r *Registry) Run(ctx context.Context, onReady func()) error {
	var pending atomic.Int64
	pending.Store(int64(len(r.runnables)))
	declare := func() {
		if pending.Add(-1) == 0 && onReady != nil {
			onReady()
		}
	}
	if len(r.runnables) == 0 && onReady != nil {
		onReady()
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, run := range r.runnables {
		ready := sync.OnceFunc(declare)
		g.Go(func() error {
			err := run.Run(gctx, ready)
			if errors.Is(err, context.Canceled) && gctx.Err() != nil {
				return nil
			}
			return err
		})
	}
	return g.Wait()
}
