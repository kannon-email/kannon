package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
//
// A read that failed (requireRead) leaves the evaluation inconclusive rather than failed: the
// condition was never checked, so a hold does not count it as a violation.
type probe struct {
	failures     []string
	failed       bool
	inconclusive bool
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

func (p *probe) readFailedf(format string, args ...any) {
	p.inconclusive = true
	p.failures = append(p.failures, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (p *probe) passed() bool {
	return !p.failed && !p.inconclusive
}

func (p *probe) String() string {
	return strings.Join(p.failures, "\n")
}

// requireRead checks that a read of a public output got an answer. Inside a wait, a transport error
// ends the evaluation as inconclusive, to be retried.
func requireRead(c require.TestingT, err error) {
	if p, ok := c.(*probe); ok && err != nil {
		p.readFailedf("read failed: %v", err)
	}
	require.NoError(c, err)
}

// requireOK checks that a read was answered 200. Inside a wait, a 5xx ends the evaluation as
// inconclusive, to be retried; any other status is a failure of the condition.
func requireOK(c require.TestingT, got int, body []byte) {
	if p, ok := c.(*probe); ok && got >= http.StatusInternalServerError {
		p.readFailedf("read answered %d: %s", got, body)
	}
	require.Equal(c, http.StatusOK, got, string(body))
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
		if p.passed() {
			return
		}
		if time.Now().After(deadline) || t.Context().Err() != nil {
			t.Fatalf("%s: not within %s:\n%s", what, timeout, p)
		}
		time.Sleep(pollInterval)
	}
}

// holds asserts that cond stays true for the whole hold window, failing t on the first evaluation
// that finds it false. An evaluation whose read failed is retried, and the hold needs at least one
// evaluation that read successfully: when the window closes without one, it keeps trying until the
// policy's timeout.
func holds(t *testing.T, what string, cond func(c *probe)) {
	t.Helper()
	start := time.Now()
	window := start.Add(scaled(holdWindow))
	timeout := start.Add(scaled(defaultTimeout))
	checked := false
	for {
		p := evaluate(cond)
		switch {
		case p.failed:
			t.Fatalf("%s: stopped holding:\n%s", what, p)
		case p.inconclusive:
			if time.Now().After(timeout) || t.Context().Err() != nil {
				t.Fatalf("%s: could not be checked within %s:\n%s", what, scaled(defaultTimeout), p)
			}
		default:
			checked = true
		}
		if checked && time.Now().After(window) {
			return
		}
		time.Sleep(pollInterval)
	}
}
