package runner_test

import (
	"context"
	"testing"
	"time"

	"github.com/ludusrusso/kannon/internal/runner"
	"github.com/stretchr/testify/assert"
)

type testLooper struct {
	count uint
}

func (t *testLooper) Loop(ctx context.Context) error {
	t.count += 1
	return nil
}

func TestMaxLoops(t *testing.T) {
	ctx := context.Background()
	l := &testLooper{}
	err := runner.Run(ctx, l.Loop, runner.MaxLoop(1))
	assert.Nil(t, err)
	assert.Equal(t, uint(1), l.count)
}

func TestMaxLoops10(t *testing.T) {
	ctx := context.Background()
	l := &testLooper{}
	err := runner.Run(ctx, l.Loop, runner.MaxLoop(10))
	assert.Nil(t, err)
	assert.Equal(t, uint(10), l.count)
}

func TestRunWithoutMaxLoopSholdEndWithErrContextExpired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	l := &testLooper{}
	err := runner.Run(ctx, l.Loop)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, uint(1), l.count)
}

func TestWithLoopWaitBig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	l := &testLooper{}
	err := runner.Run(ctx, l.Loop, runner.WaitLoop(100*time.Microsecond))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Greater(t, l.count, uint(1))
}

func TestWithLoopWaitSmall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()

	l := &testLooper{}
	err := runner.Run(ctx, l.Loop, runner.WaitLoop(100*time.Second))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, l.count, uint(1))
}
