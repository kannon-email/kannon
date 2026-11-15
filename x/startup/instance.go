package startup

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/kannon-email/kannon/x/container"
)

// errStoppedBeforeReady is what Ready reports for an instance whose components all returned, without
// failing, before every one of them was ready.
var errStoppedBeforeReady = errors.New("kannon stopped before every component was ready")

type instance struct {
	addrs          Addrs
	ready          chan struct{}
	done           chan struct{}
	runErr         error
	cancel         context.CancelFunc
	listeners      []io.Closer
	closeContainer func() error
	closeOnce      sync.Once
	closeErr       error
}

// newInstance prepares an instance whose ready channel can be handed to components before launch.
// listeners and closeContainer are closed on Close, once every component has returned.
func newInstance(addrs Addrs, listeners []io.Closer, closeContainer func() error) *instance {
	return &instance{
		addrs:          addrs,
		ready:          make(chan struct{}),
		done:           make(chan struct{}),
		cancel:         func() {},
		listeners:      listeners,
		closeContainer: closeContainer,
	}
}

// launch runs every component of reg and returns at once.
func (i *instance) launch(ctx context.Context, reg *container.Registry) {
	ctx, i.cancel = context.WithCancel(ctx)
	go func() {
		defer close(i.done)
		i.runErr = reg.Run(ctx, func() { close(i.ready) })
	}()
}

func (i *instance) Addrs() Addrs { return i.addrs }

func (i *instance) Ready(ctx context.Context) error {
	select {
	case <-i.ready:
		return nil
	case <-i.done:
		select {
		case <-i.ready:
			return nil
		default:
		}
		if i.runErr != nil {
			return i.runErr
		}
		return errStoppedBeforeReady
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (i *instance) Wait() error {
	<-i.done
	return i.runErr
}

func (i *instance) Close() error {
	i.closeOnce.Do(func() {
		i.cancel()
		<-i.done
		// A component that served a listener has closed it already; one that returned early has not.
		for _, l := range i.listeners {
			_ = l.Close()
		}
		i.closeErr = i.closeContainer()
	})
	return i.closeErr
}
