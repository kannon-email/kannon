package sqlc

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/domains"
	"github.com/stretchr/testify/require"
)

func TestDomainsRepository(t *testing.T) {
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM domains CASCADE")
	})
	_, err := db.Exec(context.Background(), "DELETE FROM domains CASCADE")
	require.NoError(t, err)

	repo := NewDomainsRepository(q)
	domains.RunRepoSpec(t, repo)
}
