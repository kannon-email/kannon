package container

import (
	"context"
	"fmt"
	"time"
)

type CloserFunc func(context.Context) error

func (c *Container) addClosers(closers ...CloserFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closers = append(c.closers, closers...)
}

func (c *Container) CloseWithTimeout(timeout time.Duration) error {
	c.mu.Lock()
	closers := append([]CloserFunc(nil), c.closers...)
	c.mu.Unlock()

	if len(closers) == 0 {
		return nil
	}

	errCh := make(chan error, len(closers))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Close resources in parallel
	for _, closer := range closers {
		go func(fn CloserFunc) {
			errCh <- fn(ctx)
		}(closer)
	}

	var errs []error
	for range closers {
		select {
		case err := <-errCh:
			if err != nil {
				errs = append(errs, err)
			}
		case <-ctx.Done():
			return fmt.Errorf("shutdown timeout exceeded: %w", ctx.Err())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}
