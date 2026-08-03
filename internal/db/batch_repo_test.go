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
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
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
	repo := NewBatchRepository(db)
	batch.RunRepoSpec(t, repo, batchTestHelper{})
}

// A Batch is not created against a Template that is not there. Intake looks the
// Template up first, so this is the race that lookup cannot close — and letting
// the Batch through is what used to produce Deliveries no Envelope could be
// built for (ADR 0008).
func TestBatchCreateRefusedWithoutItsTemplate(t *testing.T) {
	domainName := batchTestHelper{}.CreateDomain(t)

	b, err := batch.New(batch.NewParams{
		Domain:     domainName,
		Subject:    "hello",
		Sender:     batch.Sender{Email: "from@" + domainName, Alias: "From"},
		TemplateID: "tpl_deleted_before_the_insert",
	})
	require.NoError(t, err)

	err = NewBatchRepository(db).Create(t.Context(), b)
	require.ErrorIs(t, err, batch.ErrTemplateMissing)
}
