package container

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	testValue   = "test-value"
	initialized = "initialized"
)

func TestSingleton_SuccessfulInitialization(t *testing.T) {
	s := &singleton[string]{}
	ctx := context.Background()

	expected := testValue
	initFunc := func(ctx context.Context) (string, error) {
		return expected, nil
	}

	result := s.MustGet(ctx, initFunc)

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	if s.err != nil {
		t.Errorf("Expected no error, got %v", s.err)
	}
}

func TestSingleton_OnlyInitializedOnce(t *testing.T) {
	s := &singleton[int]{}
	ctx := context.Background()

	callCount := 0
	initFunc := func(ctx context.Context) (int, error) {
		callCount++
		return callCount, nil
	}

	// Call Get multiple times
	result1 := s.MustGet(ctx, initFunc)
	result2 := s.MustGet(ctx, initFunc)
	result3 := s.MustGet(ctx, initFunc)

	if result1 != 1 {
		t.Errorf("Expected first call to return 1, got %d", result1)
	}

	if result2 != 1 {
		t.Errorf("Expected second call to return 1 (cached), got %d", result2)
	}

	if result3 != 1 {
		t.Errorf("Expected third call to return 1 (cached), got %d", result3)
	}

	if callCount != 1 {
		t.Errorf("Expected initialization function to be called once, was called %d times", callCount)
	}
}

func TestSingleton_ConcurrentAccess(t *testing.T) {
	s := &singleton[string]{}
	ctx := context.Background()

	callCount := 0
	var mu sync.Mutex

	initFunc := func(_ context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		time.Sleep(10 * time.Millisecond) // Simulate some work
		return initialized, nil
	}

	const numGoroutines = 10
	results := make(chan string, numGoroutines)

	// Start multiple goroutines trying to initialize concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			result := s.MustGet(ctx, initFunc)
			results <- result
		}()
	}

	// Collect all results
	for i := 0; i < numGoroutines; i++ {
		result := <-results
		if result != initialized {
			t.Errorf("Expected %q, got %s", initialized, result)
		}
	}

	// Verify initialization function was called exactly once
	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("Expected initialization function to be called once, was called %d times", callCount)
	}
}

func TestSingleton_StructType(t *testing.T) {
	type testStruct struct {
		Name string
		ID   int
	}

	s := &singleton[testStruct]{}
	ctx := context.Background()

	expected := testStruct{Name: "test", ID: 42}
	initFunc := func(ctx context.Context) (testStruct, error) {
		return expected, nil
	}

	result := s.MustGet(ctx, initFunc)

	if result.Name != expected.Name || result.ID != expected.ID {
		t.Errorf("Expected %+v, got %+v", expected, result)
	}
}

func TestSingleton_PointerType(t *testing.T) {
	type testStruct struct {
		Name string
		ID   int
	}

	s := &singleton[*testStruct]{}
	ctx := context.Background()

	expected := &testStruct{Name: "test", ID: 42}
	initFunc := func(ctx context.Context) (*testStruct, error) {
		return expected, nil
	}

	result := s.MustGet(ctx, initFunc)

	if result == nil {
		t.Fatal("Expected non-nil result")
		return
	}

	if result.Name != expected.Name || result.ID != expected.ID {
		t.Errorf("Expected %+v, got %+v", expected, result)
	}

	// Verify it's the same instance
	if result != expected {
		t.Error("Expected same pointer instance")
	}
}

type testInterface interface {
	GetValue() string
}

type testImpl struct {
	value string
}

func (ti *testImpl) GetValue() string {
	return ti.value
}

func TestSingleton_InterfaceType(t *testing.T) {
	s := &singleton[testInterface]{}
	ctx := context.Background()

	expected := &testImpl{value: "test-interface"}
	initFunc := func(ctx context.Context) (testInterface, error) {
		return expected, nil
	}

	result := s.MustGet(ctx, initFunc)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.GetValue() != "test-interface" {
		t.Errorf("Expected 'test-interface', got %s", result.GetValue())
	}
}

type contextKey string

const testKey contextKey = "key"

func TestSingleton_ContextPassing(t *testing.T) {
	s := &singleton[string]{}
	ctx := context.WithValue(context.Background(), testKey, testValue)

	var receivedCtx context.Context
	initFunc := func(ctx context.Context) (string, error) {
		receivedCtx = ctx
		return "result", nil
	}

	s.MustGet(ctx, initFunc)

	if receivedCtx.Value(testKey) != testValue {
		t.Error("Context was not properly passed to initialization function")
	}
}

// Note: Testing error handling would require intercepting logrus.Fatalf
// or modifying the singleton to make it testable, which might break the API
// For now, we focus on testing the successful path and concurrent behavior
