package container

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

type CloserFunc func(context.Context) error

func (c *Container) addClosers(closers ...CloserFunc) {
	c.closers = append(c.closers, closers...)
}

func (c *Container) CloseWithTimeout(timeout time.Duration) error {
	if len(c.closers) == 0 {
		return nil
	}

	errCh := make(chan error, len(c.closers))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Close resources in parallel
	for _, closer := range c.closers {
		go func(fn CloserFunc) {
			errCh <- fn(ctx)
		}(closer)
	}

	var errs []error
	for i := 0; i < len(c.closers); i++ {
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

const closeTimeout = 30 * time.Second

// Close keeps existing Close() for backward compatibility
func (c *Container) Close() {
	if err := c.CloseWithTimeout(closeTimeout); err != nil {
		logrus.Errorf("Shutdown errors: %v", err)
	}
}
