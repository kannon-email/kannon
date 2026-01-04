package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/stretchr/testify/require"
)

func TestAPIKeysRepository(t *testing.T) {
	repo := NewAPIKeysRepository(q, db)

	helper := apikeys.RepoTestHelper{
		CreateDomain: func(t *testing.T) string {
			ctx := context.Background()
			domainName := fmt.Sprintf("test-apikeys-%d.com", time.Now().UnixNano())

			// Create domain directly using queries
			_, err := q.CreateDomain(ctx, CreateDomainParams{
				Domain:         domainName,
				Key:            "test-key",
				DkimPrivateKey: "test-private",
				DkimPublicKey:  "test-public",
			})
			require.NoError(t, err)
			return domainName
		},
		CleanDB: func(t *testing.T) {
			ctx := context.Background()
			_, err := db.Exec(ctx, "TRUNCATE api_keys CASCADE")
			require.NoError(t, err)
			_, err = db.Exec(ctx, "TRUNCATE domains CASCADE")
			require.NoError(t, err)
		},
	}

	apikeys.RunRepoSpec(t, repo, helper)
}
