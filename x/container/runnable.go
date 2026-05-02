package container

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Runnable describes one component the application starts. Name is used for
// logging and diagnostics; Run is the long-running entry point and must return
// when ctx is cancelled.
type Runnable struct {
	Name string
	Run  func(ctx context.Context) error
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
// non-nil error.
func (r *Registry) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, run := range r.runnables {
		run := run
		g.Go(func() error {
			return run.Run(gctx)
		})
	}
	return g.Wait()
}
