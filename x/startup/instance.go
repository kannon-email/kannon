package startup

import (
	"context"
	"sync"

	"github.com/kannon-email/kannon/x/container"
)

type instance struct {
	addrs          Addrs
	ready          chan struct{}
	done           chan struct{}
	runErr         error
	cancel         context.CancelFunc
	closeContainer func() error
	closeOnce      sync.Once
	closeErr       error
}

// launch runs every component of reg and returns at once. closeContainer runs on Close, once every
// component has returned.
func launch(ctx context.Context, reg *container.Registry, addrs Addrs, closeContainer func() error) *instance {
	ctx, cancel := context.WithCancel(ctx)
	inst := &instance{
		addrs:          addrs,
		ready:          make(chan struct{}),
		done:           make(chan struct{}),
		cancel:         cancel,
		closeContainer: closeContainer,
	}
	go func() {
		defer close(inst.done)
		inst.runErr = reg.Run(ctx, func() { close(inst.ready) })
	}()
	return inst
}

func (i *instance) Addrs() Addrs { return i.addrs }

func (i *instance) Ready() <-chan struct{} { return i.ready }

func (i *instance) Wait() error {
	<-i.done
	return i.runErr
}

func (i *instance) Close() error {
	i.closeOnce.Do(func() {
		i.cancel()
		<-i.done
		i.closeErr = i.closeContainer()
	})
	return i.closeErr
}
