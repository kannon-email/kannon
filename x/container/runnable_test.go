package container

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistry_RegisterAndNames(t *testing.T) {
	var reg Registry
	reg.Register(Runnable{Name: "a", Run: func(ctx context.Context) error { return nil }})
	reg.Register(Runnable{Name: "b", Run: func(ctx context.Context) error { return nil }})

	got := reg.Names()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("unexpected names: %v", got)
	}
}

func TestRegistry_RunInvokesAllAndShareCtx(t *testing.T) {
	var reg Registry
	var ranA, ranB atomic.Bool
	type ctxKey struct{}
	parent := context.WithValue(context.Background(), ctxKey{}, "shared")
	parent, cancel := context.WithCancel(parent)

	reg.Register(Runnable{Name: "a", Run: func(ctx context.Context) error {
		ranA.Store(true)
		if ctx.Value(ctxKey{}) != "shared" {
			t.Errorf("a: did not see parent ctx value")
		}
		<-ctx.Done()
		return nil
	}})
	reg.Register(Runnable{Name: "b", Run: func(ctx context.Context) error {
		ranB.Store(true)
		if ctx.Value(ctxKey{}) != "shared" {
			t.Errorf("b: did not see parent ctx value")
		}
		<-ctx.Done()
		return nil
	}})

	done := make(chan error, 1)
	go func() { done <- reg.Run(parent) }()

	// Give goroutines a moment to start.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) && err != nil {
			t.Errorf("expected ctx.Canceled or nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if !ranA.Load() || !ranB.Load() {
		t.Errorf("expected both runnables to run, got a=%v b=%v", ranA.Load(), ranB.Load())
	}
}

func TestRegistry_ErrorCancelsSibling(t *testing.T) {
	var reg Registry
	boom := errors.New("boom")
	siblingDone := make(chan struct{})

	reg.Register(Runnable{Name: "fail", Run: func(ctx context.Context) error {
		return boom
	}})
	reg.Register(Runnable{Name: "sibling", Run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingDone)
		return ctx.Err()
	}})

	err := reg.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("expected boom, got %v", err)
	}
	select {
	case <-siblingDone:
	case <-time.After(time.Second):
		t.Fatal("sibling was not cancelled")
	}
}
