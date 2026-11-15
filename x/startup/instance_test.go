package startup

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kannon-email/kannon/x/container"
)

func TestCloseWaitsForEveryComponentBeforeClosingTheContainer(t *testing.T) {
	var running atomic.Int32
	reg := &container.Registry{}
	for _, name := range []string{"quick", "slow"} {
		running.Add(1)
		reg.Register(container.Runnable{Name: name, Run: func(ctx context.Context, ready func()) error {
			defer running.Add(-1)
			ready()
			<-ctx.Done()
			if name == "slow" {
				time.Sleep(50 * time.Millisecond)
			}
			return nil
		}})
	}

	var stillRunningAtClose int32 = -1
	inst := newInstance(Addrs{}, nil, func() error {
		stillRunningAtClose = running.Load()
		return nil
	})
	inst.launch(t.Context(), reg)
	if err := inst.Ready(t.Context()); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	if err := inst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if stillRunningAtClose != 0 {
		t.Fatalf("the container was closed with %d components still running", stillRunningAtClose)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("a second Close must be harmless, got %v", err)
	}
}

func TestWaitReportsTheComponentThatFailed(t *testing.T) {
	boom := errors.New("boom")
	reg := &container.Registry{}
	reg.Register(container.Runnable{Name: "failing", Run: func(context.Context, func()) error { return boom }})

	inst := newInstance(Addrs{}, nil, func() error { return nil })
	inst.launch(t.Context(), reg)
	t.Cleanup(func() { _ = inst.Close() })

	if err := inst.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait = %v, want %v", err, boom)
	}
}

func TestReadyReportsAComponentThatFailedBeforeBeingReady(t *testing.T) {
	boom := errors.New("boom")
	reg := &container.Registry{}
	reg.Register(container.Runnable{Name: "healthy", Run: func(ctx context.Context, ready func()) error {
		ready()
		<-ctx.Done()
		return nil
	}})
	reg.Register(container.Runnable{Name: "failing", Run: func(context.Context, func()) error { return boom }})

	inst := newInstance(Addrs{}, nil, func() error { return nil })
	inst.launch(t.Context(), reg)
	t.Cleanup(func() { _ = inst.Close() })

	if err := readyWithin(t, inst); !errors.Is(err, boom) {
		t.Fatalf("Ready = %v, want %v", err, boom)
	}
}

func TestReadyFailsWhenAComponentReturnsWithoutBeingReady(t *testing.T) {
	reg := &container.Registry{}
	reg.Register(container.Runnable{Name: "quitter", Run: func(context.Context, func()) error { return nil }})

	inst := newInstance(Addrs{}, nil, func() error { return nil })
	inst.launch(t.Context(), reg)
	t.Cleanup(func() { _ = inst.Close() })

	if err := readyWithin(t, inst); err == nil {
		t.Fatal("an instance that stopped before every component was ready must not report ready")
	}
}

func TestReadyStopsWaitingWhenItsContextIsDone(t *testing.T) {
	reg := &container.Registry{}
	reg.Register(container.Runnable{Name: "never-ready", Run: func(ctx context.Context, _ func()) error {
		<-ctx.Done()
		return nil
	}})

	inst := newInstance(Addrs{}, nil, func() error { return nil })
	inst.launch(t.Context(), reg)
	t.Cleanup(func() { _ = inst.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := inst.Ready(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ready = %v, want %v", err, context.DeadlineExceeded)
	}
}

func readyWithin(t *testing.T, inst *instance) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := inst.Ready(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Ready kept waiting on an instance that had already stopped")
	}
	return err
}

func TestCloseClosesAListenerNoComponentServed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reg := &container.Registry{}
	reg.Register(container.Runnable{Name: "early-return", Run: func(context.Context, func()) error {
		return errors.New("gave up before serving")
	}})

	inst := newInstance(Addrs{}, []io.Closer{l}, func() error { return nil })
	inst.launch(t.Context(), reg)
	_ = inst.Wait()
	_ = inst.Close()

	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close = %v, want %v", err, net.ErrClosed)
	}
}
