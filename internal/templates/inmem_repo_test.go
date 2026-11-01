package templates

import (
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// inMemHelper needs no Domain row: the in-memory Repository, like the sqlc-backed one, does not
// check that the Domain a Template names exists.
type inMemHelper struct{}

func (inMemHelper) CreateDomain(*testing.T) values.DomainName {
	return values.MustParse(fmt.Sprintf("templates-%d.test", time.Now().UnixNano()))
}

func TestInMemRepository(t *testing.T) {
	RunRepoSpec(t, NewInMemRepository(), inMemHelper{})
}
