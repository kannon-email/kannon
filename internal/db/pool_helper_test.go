package sqlc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/require"
)

// seedBatchFixture seeds a fresh domain + template + batch row so
// foreign-key constraints on sending_pool_emails are satisfied. Returns
// the BatchID and Domain name. Used by repository specification tests
// for the delivery / pool packages.
func seedBatchFixture(t *testing.T) (batch.ID, string) {
	t.Helper()
	ctx := t.Context()
	domainName := fmt.Sprintf("test-pool-%d.com", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(t, err)

	tplID := fmt.Sprintf("tpl_%d", time.Now().UnixNano())
	_, err = q.CreateTemplate(ctx, CreateTemplateParams{
		TemplateID: tplID,
		Html:       "<p>hi</p>",
		Domain:     domainName,
		Type:       TemplateTypeTransient,
	})
	require.NoError(t, err)

	bID := batch.NewID(domainName)
	_, err = q.CreateMessage(ctx, CreateMessageParams{
		MessageID:   bID.String(),
		Subject:     "hello",
		SenderEmail: "from@" + domainName,
		SenderAlias: "From",
		TemplateID:  tplID,
		Domain:      domainName,
		Attachments: Attachments{},
		Headers:     Headers{},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.Exec(cleanupCtx, "DELETE FROM sending_pool_emails WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		_, _ = db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})

	return bID, domainName
}
