package e2e_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHoldsRetriesAReadThatFailed(t *testing.T) {
	var evaluations int
	holds(t, "a condition whose first reads fail", func(c *probe) {
		evaluations++
		switch evaluations {
		case 1:
			requireRead(c, errors.New("connection refused"))
		case 2:
			requireStatus(c, http.StatusServiceUnavailable, http.StatusOK, []byte("try later"))
		}
		require.True(c, true)
	})
	if evaluations < 3 {
		t.Fatalf("holds stopped after %d evaluations", evaluations)
	}
}

func TestEventuallyRetriesAReadThatFailed(t *testing.T) {
	var evaluations int
	eventually(t, "a condition whose first read fails", func(c *probe) {
		evaluations++
		if evaluations == 1 {
			requireRead(c, errors.New("connection refused"))
		}
	})
}
