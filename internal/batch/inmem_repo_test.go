package batch

import (
	"fmt"
	"testing"
	"time"
)

// inMemHelper creates no rows: neither the in-memory Repository nor the sqlc-backed one checks that
// the Domain or Template a Batch names exists.
type inMemHelper struct{}

func (inMemHelper) CreateDomain(*testing.T) string {
	return fmt.Sprintf("batch-%d.test", time.Now().UnixNano())
}

func (inMemHelper) CreateTemplate(_ *testing.T, domain string) string {
	return fmt.Sprintf("template_%d@%s", time.Now().UnixNano(), domain)
}

func TestInMemRepository(t *testing.T) {
	RunRepoSpec(t, NewInMemRepository(), inMemHelper{})
}
