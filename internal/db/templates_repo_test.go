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
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})
	return domainName
}

func TestTemplatesRepository(t *testing.T) {
	repo := NewTemplatesRepository(db)
	templates.RunRepoSpec(t, repo, templatesTestHelper{})
}

// Delete refuses a Template a Batch still references, and says so in the
// Template vocabulary rather than as a database error (ADR 0008). The Batch
// resolves its body through the Template row every time an Envelope is built,
// so the row has to outlive the Batch.
func TestTemplateDeleteRefusedWhileBatchReferencesIt(t *testing.T) {
	ctx := t.Context()
	repo := NewTemplatesRepository(db)

	batchID, _ := seedBatchFixture(t)
	seeded, err := q.GetMessage(ctx, batchID.String())
	require.NoError(t, err)

	_, err = repo.Delete(ctx, seeded.TemplateID)
	require.ErrorIs(t, err, templates.ErrTemplateInUse)

	kept, err := repo.GetByID(ctx, seeded.TemplateID)
	require.NoError(t, err)
	require.Equal(t, seeded.TemplateID, kept.TemplateID())
}
