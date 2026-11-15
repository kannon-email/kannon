package container

import (
	"context"
	"testing"
	"time"
)

func TestRegistry_ReadyFiresOnlyOnceEveryRunnableIsReady(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	release := make(chan struct{})
	var reg Registry
	reg.Register(Runnable{Name: "fast", Run: func(ctx context.Context, ready func()) error {
		ready()
		<-ctx.Done()
		return nil
	}})
	reg.Register(Runnable{Name: "slow", Run: func(ctx context.Context, ready func()) error {
		<-release
		ready()
		ready()
		<-ctx.Done()
		return nil
	}})

	fired := make(chan struct{})
	go func() {
		if err := reg.Run(ctx, func() { close(fired) }); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	select {
	case <-fired:
		t.Fatal("ready fired before every runnable declared readiness")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("ready never fired")
	}
}
