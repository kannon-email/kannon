package runner

import (
	"context"
	"time"
)

type loopFunc func(context.Context) error

type RunOpts interface {
	contifigureRunner(opts *runOpts)
}

func Run(ctx context.Context, loop loopFunc, confOptions ...RunOpts) error {
	opts := buildOptions(confOptions)
	var runs uint = 0

	for {
		if err := loop(ctx); err != nil {
			return err
		}
		runs += 1
		if checkExit(runs, opts) {
			return nil
		}
		select {
		case <-time.After(opts.loopWait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func buildOptions(opts []RunOpts) runOpts {
	o := runOpts{
		loopWait: 10 * time.Millisecond,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.contifigureRunner(&o)
	}
	return o
}

func checkExit(runs uint, opt runOpts) bool {
	mlo := opt.maxLoops
	return mlo.hasLimit && runs >= mlo.maxLoop
}

type runOpts struct {
	maxLoops maxLoopsOpt
	loopWait time.Duration
}

type maxLoopsOpt struct {
	hasLimit bool
	maxLoop  uint
}

func MaxLoop(max uint) RunOpts {
	return configureMaxLoops{
		maxLoops: max,
	}
}

type configureMaxLoops struct {
	maxLoops uint
}

func (c configureMaxLoops) contifigureRunner(opts *runOpts) {
	opts.maxLoops = maxLoopsOpt{
		hasLimit: true,
		maxLoop:  c.maxLoops,
	}
}

func WaitLoop(wait time.Duration) RunOpts {
	return configureLoopWait{
		wait: wait,
	}
}

type configureLoopWait struct {
	wait time.Duration
}

func (c configureLoopWait) contifigureRunner(opts *runOpts) {
	opts.loopWait = c.wait
}
