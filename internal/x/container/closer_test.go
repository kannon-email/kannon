package container

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestContainer_CloseWithTimeout_EmptyClosers(t *testing.T) {
	container := &Container{closers: []CloserFunc{}}

	err := container.CloseWithTimeout(5 * time.Second)
	if err != nil {
		t.Errorf("Expected no error for empty closers, got %v", err)
	}
}

func TestContainer_CloseWithTimeout_SuccessfulClose(t *testing.T) {
	var calls int32
	container := &Container{}

	// Add multiple closers
	container.addClosers(
		func(_ context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
		func(_ context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
		func(_ context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	)

	err := container.CloseWithTimeout(5 * time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("Expected 3 calls, got %d", calls)
	}
}

func TestContainer_CloseWithTimeout_ParallelExecution(t *testing.T) {
	container := &Container{}

	// Add closers that take some time
	var wg sync.WaitGroup
	startTime := time.Now()

	for range 5 {
		container.addClosers(func(_ context.Context) error {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(100 * time.Millisecond)
			}()
			return nil
		})
	}

	err := container.CloseWithTimeout(5 * time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	// Should complete in roughly 100ms (parallel) rather than 500ms (sequential)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Expected parallel execution to complete in ~100ms, took %v", elapsed)
	}
}

func TestContainer_CloseWithTimeout_ErrorAggregation(t *testing.T) {
	container := &Container{}

	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	container.addClosers(
		func(_ context.Context) error { return err1 },
		func(_ context.Context) error { return nil },
		func(_ context.Context) error { return err2 },
	)

	err := container.CloseWithTimeout(5 * time.Second)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "shutdown errors") {
		t.Errorf("Expected 'shutdown errors' in error message, got %v", err)
	}

	if !strings.Contains(errStr, "error 1") || !strings.Contains(errStr, "error 2") {
		t.Errorf("Expected both errors in aggregated error, got %v", err)
	}
}

func TestContainer_CloseWithTimeout_TimeoutExceeded(t *testing.T) {
	container := &Container{}

	// Add a closer that takes longer than the timeout
	container.addClosers(func(_ context.Context) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	err := container.CloseWithTimeout(50 * time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "shutdown timeout exceeded") {
		t.Errorf("Expected timeout error message, got %v", err)
	}
}

func TestContainer_CloseWithTimeout_MixedErrorsAndTimeout(t *testing.T) {
	container := &Container{}

	container.addClosers(
		func(_ context.Context) error { return errors.New("quick error") },
		func(_ context.Context) error {
			time.Sleep(200 * time.Millisecond)
			return nil
		},
	)

	err := container.CloseWithTimeout(50 * time.Millisecond)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Should get timeout error (since timeout is checked first)
	if !strings.Contains(err.Error(), "shutdown timeout exceeded") {
		t.Errorf("Expected timeout error, got %v", err)
	}
}

func TestContainer_Close_BackwardCompatibility(t *testing.T) {
	container := &Container{}
	var called bool

	container.addClosers(func(_ context.Context) error {
		called = true
		return nil
	})

	// Should not panic and should call the closer
	container.Close()

	if !called {
		t.Error("Expected closer to be called")
	}
}

func TestContainer_Close_ErrorLogging(t *testing.T) {
	container := &Container{}

	// Add a closer that returns an error
	container.addClosers(func(_ context.Context) error {
		return errors.New("test error")
	})

	// Should not panic even with errors
	container.Close()
}

func TestContainer_CloseWithTimeout_ConcurrentClosers(t *testing.T) {
	container := &Container{}

	const numClosers = 100
	var counter int64

	// Add many closers that increment a counter
	for range numClosers {
		container.addClosers(func(_ context.Context) error {
			atomic.AddInt64(&counter, 1)
			time.Sleep(1 * time.Millisecond) // Small delay to test concurrency
			return nil
		})
	}

	err := container.CloseWithTimeout(5 * time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if atomic.LoadInt64(&counter) != numClosers {
		t.Errorf("Expected %d closers to be called, got %d", numClosers, counter)
	}
}

func TestContainer_CloseWithTimeout_ZeroTimeout(t *testing.T) {
	container := &Container{}

	container.addClosers(func(_ context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	err := container.CloseWithTimeout(0)
	if err == nil {
		t.Fatal("Expected timeout error with zero timeout, got nil")
	}

	if !strings.Contains(err.Error(), "shutdown timeout exceeded") {
		t.Errorf("Expected timeout error, got %v", err)
	}
}

func TestContainer_addClosers(t *testing.T) {
	container := &Container{}

	closer1 := func(_ context.Context) error { return nil }
	closer2 := func(_ context.Context) error { return nil }
	closer3 := func(_ context.Context) error { return nil }

	// Test adding single closer
	container.addClosers(closer1)
	if len(container.closers) != 1 {
		t.Errorf("Expected 1 closer, got %d", len(container.closers))
	}

	// Test adding multiple closers
	container.addClosers(closer2, closer3)
	if len(container.closers) != 3 {
		t.Errorf("Expected 3 closers, got %d", len(container.closers))
	}
}
