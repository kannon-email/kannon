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
// and benchmarks for the delivery / pool packages.
func seedBatchFixture(tb testing.TB) (batch.ID, string) {
	tb.Helper()
	ctx := context.Background()
	domainName := fmt.Sprintf("test-pool-%d.com", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, CreateDomainParams{
		Domain:         domainName,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(tb, err)

	tplID := fmt.Sprintf("tpl_%d", time.Now().UnixNano())
	_, err = q.CreateTemplate(ctx, CreateTemplateParams{
		TemplateID: tplID,
		Html:       "<p>hi</p>",
		Domain:     domainName,
		Type:       TemplateTypeTransient,
	})
	require.NoError(tb, err)

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
	require.NoError(tb, err)

	tb.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM sending_pool_emails WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domainName)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domainName)
	})

	return bID, domainName
}
