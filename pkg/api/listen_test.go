package api

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/spf13/viper"
)

func TestListenBindsPortZeroOnceForEachListener(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("api", map[string]any{"port": 0, "rest_port": 0, "health_port": 0})

	l, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	seen := map[int]bool{}
	for _, ln := range []net.Listener{l.Connect, l.REST, l.Health} {
		port := ln.Addr().(*net.TCPAddr).Port //nolint:errcheck // a TCP listener has a TCP address
		if port == 0 || seen[port] {
			t.Fatalf("expected three distinct system-picked ports, got %v", ln.Addr())
		}
		seen[port] = true
	}
}

func TestReadinessIsUnavailableUntilTheInstanceIsReady(t *testing.T) {
	instanceReady := make(chan struct{})
	healthy := func(context.Context) error { return nil }
	check := readiness(instanceReady, healthy)

	if err := check(t.Context()); err == nil {
		t.Fatal("expected not ready before the instance is")
	}

	close(instanceReady)
	if err := check(t.Context()); err != nil {
		t.Fatalf("expected ready once the instance is, got %v", err)
	}

	broken := readiness(instanceReady, func(context.Context) error { return errors.New("db down") })
	if err := broken(t.Context()); err == nil {
		t.Fatal("expected a failing dependency to be reported after readiness")
	}
}
