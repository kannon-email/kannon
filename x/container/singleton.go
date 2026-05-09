package container

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"sync"
)

type singleton[T any] struct {
	once  sync.Once
	value T
	err   error
}

type makeFn[T any] func(ctx context.Context) (T, error)

func (s *singleton[T]) Get(ctx context.Context, f makeFn[T]) (T, error) {
	s.once.Do(func() {
		s.value, s.err = f(ctx)
	})
	return s.value, s.err
}

func (s *singleton[T]) MustGet(ctx context.Context, f makeFn[T]) T {
	value, err := s.Get(ctx, f)
	if err != nil {
		typeName := getTypeName[T]()
		slog.Error("Failed to initialize singleton of type", "type", typeName, "err", err)
		os.Exit(1)
	}
	return value
}

func getTypeName[T any]() string {
	var zero T
	tf := reflect.TypeOf(zero)
	if tf.Kind() == reflect.Pointer {
		tf = tf.Elem()
	}
	return tf.String()
}
