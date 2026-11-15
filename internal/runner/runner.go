package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/internal/transient"
)

type loopFunc func(context.Context) error

type RunOpts interface {
	configureRunner(opts *runOpts)
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

// Tolerate wraps a periodic cycle so that its failure is logged and the next cycle runs anyway,
// instead of ending Run and, with it, the process. A cycle that reads Postgres or NATS fails whenever
// either restarts, and a worker that exits on that turns a few seconds of downtime into a crash
// loop. log should name the cycle; how loudly each failure is reported is transient.Streak's call.
func Tolerate(log *slog.Logger, loop loopFunc) loopFunc {
	streak := transient.NewStreak(log)
	return func(ctx context.Context) error {
		err := loop(ctx)
		if err == nil {
			streak.Succeeded()
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		streak.Failed(ctx, err)
		return nil
	}
}

func buildOptions(opts []RunOpts) runOpts {
	o := runOpts{
		loopWait: 100 * time.Millisecond,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.configureRunner(&o)
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

func (c configureMaxLoops) configureRunner(opts *runOpts) {
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

func (c configureLoopWait) configureRunner(opts *runOpts) {
	opts.loopWait = c.wait
}
