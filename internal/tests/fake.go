package tests

import (
	"testing"

	"github.com/nrednav/cuid2"
)

// FakeDomain returns a unique domain name suitable for tests.
func FakeDomain(t *testing.T) string {
	t.Helper()
	return cuid2.Generate() + ".example.com"
}

// FakeUsername returns a unique username suitable for tests.
func FakeUsername(t *testing.T) string {
	t.Helper()
	return cuid2.Generate()
}

// FakeEmail returns a unique email address suitable for tests.
func FakeEmail(t *testing.T) string {
	t.Helper()
	return cuid2.Generate() + "@example.com"
}
