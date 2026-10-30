package container

import (
	"context"
	"testing"
	"time"
)

func TestRegistryCancellationIsNormalShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &Registry{}
	r.Register(Runnable{Name: "worker", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})
	if err := r.Run(ctx); err != nil {
		t.Fatalf("normal shutdown returned error: %v", err)
	}
}

func TestDependenciesCloseBeforeEmbeddedServer(t *testing.T) {
	c := &Container{}
	finished := make(chan struct{})
	c.addClosers(func(context.Context) error { close(finished); return nil })
	c.finalClosers = append(c.finalClosers, func(context.Context) error {
		select {
		case <-finished:
		default:
			t.Error("embedded server closed before its client")
		}
		return nil
	})
	if err := c.CloseWithTimeout(time.Second); err != nil {
		t.Fatal(err)
	}
}
