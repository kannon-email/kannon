package e2e_test

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The single waiting policy of the suite. Every wait and every hold is derived from these, scaled by
// KANNON_E2E_TIMEOUT_SCALE, so a slow runner needs one knob instead of edits across scenarios.
const (
	defaultTimeout = 30 * time.Second
	pollInterval   = 100 * time.Millisecond
	holdWindow     = 2 * time.Second
	timeoutScale   = "KANNON_E2E_TIMEOUT_SCALE"
)

func scaled(d time.Duration) time.Duration {
	raw := os.Getenv(timeoutScale)
	if raw == "" {
		return d
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f <= 0 {
		panic(fmt.Sprintf("%s must be a positive number, got %q", timeoutScale, raw))
	}
	return time.Duration(float64(d) * f)
}

// probe collects the failures of one evaluation of a condition. It satisfies require.TestingT, so a
// condition is written with the usual assert and require calls; a require failure ends only that
// evaluation, and the next poll tries again.
type probe struct {
	failures []string
	failed   bool
}

func (p *probe) Helper() {}

func (p *probe) Errorf(format string, args ...any) {
	p.failed = true
	p.failures = append(p.failures, fmt.Sprintf(format, args...))
}

func (p *probe) FailNow() {
	p.failed = true
	runtime.Goexit()
}

func (p *probe) String() string {
	return strings.Join(p.failures, "\n")
}

// evaluate runs one evaluation to completion before returning, so no condition outlives the wait
// that started it.
func evaluate(cond func(c *probe)) *probe {
	p := &probe{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		cond(p)
	}()
	<-done
	return p
}

// eventually polls cond until one evaluation passes, retrying every failure — a transient API error
// included — until the policy's timeout, and only then fails t with the last evaluation's failures.
func eventually(t *testing.T, what string, cond func(c *probe)) {
	t.Helper()
	timeout := scaled(defaultTimeout)
	deadline := time.Now().Add(timeout)
	for {
		p := evaluate(cond)
		if !p.failed {
			return
		}
		if time.Now().After(deadline) || t.Context().Err() != nil {
			t.Fatalf("%s: not within %s:\n%s", what, timeout, p)
		}
		time.Sleep(pollInterval)
	}
}

// holds asserts that cond stays true for the whole hold window, failing t on the first evaluation
// that does not.
func holds(t *testing.T, what string, cond func(c *probe)) {
	t.Helper()
	deadline := time.Now().Add(scaled(holdWindow))
	for time.Now().Before(deadline) {
		if p := evaluate(cond); p.failed {
			t.Fatalf("%s: stopped holding:\n%s", what, p)
		}
		time.Sleep(pollInterval)
	}
}
