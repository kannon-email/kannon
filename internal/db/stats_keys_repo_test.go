package sqlc

import (
	"testing"

	"github.com/kannon-email/kannon/internal/statssec"
)

func TestStatsKeysRepository(t *testing.T) {
	statssec.RunKeyRepoSpec(t, NewStatsKeysRepository(db))
}
