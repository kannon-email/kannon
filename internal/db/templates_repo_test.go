package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/templates"
	"github.com/stretchr/testify/require"
)

type templatesTestHelper struct{}

func (h templatesTestHelper) CreateDomain(t *testing.T) string {
	ctx := t.Context()
	domainName := fmt.Sprintf("test-tpl-%d.com", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})
	return domainName
}

func TestTemplatesRepository(t *testing.T) {
	repo := NewTemplatesRepository(q)
	templates.RunRepoSpec(t, repo, templatesTestHelper{})
}
