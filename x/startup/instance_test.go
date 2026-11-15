package startup

import (
	"context"
	"errors"
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
	inst := launch(t.Context(), reg, Addrs{}, func() error {
		stillRunningAtClose = running.Load()
		return nil
	})
	<-inst.Ready()

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

	inst := launch(t.Context(), reg, Addrs{}, func() error { return nil })
	t.Cleanup(func() { _ = inst.Close() })

	if err := inst.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait = %v, want %v", err, boom)
	}
	select {
	case <-inst.Ready():
		t.Fatal("an instance whose component never declared readiness must not be ready")
	default:
	}
}
