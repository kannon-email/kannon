package apikeyshelpers

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/kannon-email/kannon/internal/apikeys"
)

func TestInMemoryRepository(t *testing.T) {
	repo := NewInMemoryRepository()
	helper := &inMemoryTestHelper{}
	apikeys.RunRepoSpec(t, repo, helper)
}

type inMemoryTestHelper struct {
	counter atomic.Int32
}

func (h *inMemoryTestHelper) CreateDomain(t *testing.T) string {
	return fmt.Sprintf("test-domain-%d.example.com", h.counter.Add(1))
}
