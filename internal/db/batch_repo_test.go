package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/require"
)

type batchTestHelper struct{}

func (h batchTestHelper) CreateDomain(t *testing.T) string {
	ctx := t.Context()
	domainName := fmt.Sprintf("test-batch-%d.com", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})
	return domainName
}

func (h batchTestHelper) CreateTemplate(t *testing.T, domain string) string {
	ctx := t.Context()
	tplID := fmt.Sprintf("tpl_%d", time.Now().UnixNano())
	_, err := q.CreateTemplate(ctx, CreateTemplateParams{
		TemplateID: tplID,
		Html:       "<p>hi</p>",
		Domain:     domain,
		Type:       TemplateTypeTransient,
	})
	require.NoError(t, err)
	return tplID
}

func TestBatchRepository(t *testing.T) {
	repo := NewBatchRepository(q)
	batch.RunRepoSpec(t, repo, batchTestHelper{})
}
