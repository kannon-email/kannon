package sqlc

import (
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/stretchr/testify/require"
)

type testHelper struct{}

func (h testHelper) CreateDomain(t *testing.T) string {
	ctx := t.Context()
	domainName := fmt.Sprintf("test-apikeys-%d.com", time.Now().UnixNano())

	// Create domain directly using queries
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(t, err)

	return domainName
}

func TestAPIKeysRepository(t *testing.T) {
	repo := NewAPIKeysRepository(q, db)
	helper := testHelper{}
	apikeys.RunRepoSpec(t, repo, helper)
}
