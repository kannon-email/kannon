package validator_test

// Reclaiming Deliveries stranded in 'validating' (#378, ADR 0007).
//
// Drives the REAL Validator.ReclaimCycle + REAL pool.Claimer + REAL Postgres
// (dockertest, via the TestMain in validator_test.go). The fixture rows are
// deleted again: the pool is shared with every other test in this package.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
)

// strandedValidatingFor is comfortably past the Validator's threshold. The
// whole validation cycle is bounded at ten seconds, so a row this old is stuck
// by definition.
const strandedValidatingFor = 10 * time.Minute

// TestReclaimCycle_ValidatingStrandedPastThreshold_ReturnsToPoolWithoutAttemptBump
// pins both halves of the Validator's reclaim.
//
// The recovery: a Delivery claimed into 'validating' by a worker that then died
// has no other way out — nothing else writes that status — so it never reaches
// the Dispatcher at all.
//
// And the bump that must NOT happen: a reclaim from 'validating' is not a send
// attempt. Bumping the counter would silently advance the backoff curve for a
// Delivery that has never been near an MX. A permanently stranding 'validating'
// row also has no per-row cause — the Validator either passes an address or
// Drops it as Rejected — so the condition is always infrastructural and always
// transient, and looping there is the correct behaviour.
func TestReclaimCycle_ValidatingStrandedPastThreshold_ReturnsToPoolWithoutAttemptBump(t *testing.T) {
	ctx := t.Context()
	batchID, domain := seedReclaimBatch(t)

	repo := sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	claimer := pool.NewClaimer(repo)

	email := "stranded@" + domain
	claimForValidation(t, repo, claimer, batchID, domain, email)
	backdateClaim(t, batchID, email, strandedValidatingFor)

	require.NoError(t, vt.ReclaimCycle(ctx))

	got := poolRow(t, batchID, email)
	assert.Equal(t, sqlc.SendingPoolStatusToValidate, got.Status,
		"a Delivery stranded in 'validating' past the threshold must be handed back to the pool")
	assert.EqualValues(t, 0, got.SendAttemptsCnt,
		"a validating reclaim is not a send attempt and must not advance the backoff curve")
	assert.False(t, got.ClaimedAt.Valid,
		"a Delivery that is no longer in flight must not claim to be: claimed_at is cleared")
}

// seedReclaimBatch seeds the Batch row that sending_pool_emails' foreign key
// needs, plus the Domain that owns it and the Template it names, and removes
// them afterwards.
func seedReclaimBatch(t *testing.T) (batch.ID, string) {
	t.Helper()
	ctx := t.Context()

	domain := fmt.Sprintf("reclaim-%d.test", time.Now().UnixNano())
	_, err := q.CreateDomain(ctx, sqlc.CreateDomainParams{
		Domain:         domain,
		DkimPrivateKey: "test-private",
		DkimPublicKey:  "test-public",
	})
	require.NoError(t, err)

	// The Template comes first: a Batch holds a key on the one it names, so it
	// cannot be written against a Template that is not there (ADR 0008).
	tpl, err := q.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: "tpl_reclaim@" + domain,
		Html:       "<p>reclaim</p>",
		Domain:     domain,
		Type:       sqlc.TemplateTypeTransient,
	})
	require.NoError(t, err)

	b, err := batch.New(batch.NewParams{
		Domain:      domain,
		Subject:     "reclaim",
		Sender:      batch.Sender{Email: "noreply@" + domain, Alias: "Reclaim"},
		TemplateID:  tpl.TemplateID,
		Attachments: batch.Attachments{},
	})
	require.NoError(t, err)
	require.NoError(t, sqlc.NewBatchRepository(db).Create(ctx, b))

	// context.Background(), not t.Context(): the test's context is cancelled
	// before Cleanup runs, and a fixture row left behind here is picked up by
	// the next Validator cycle of another test in this package.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM sending_pool_emails WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		db.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domain)
	})

	return b.ID(), domain
}

// claimForValidation takes one Delivery through the real claim path, so the row
// under test carries exactly the state a live validation leaves behind.
func claimForValidation(t *testing.T, repo delivery.Repository, claimer pool.Claimer, batchID batch.ID, domain, email string) {
	t.Helper()
	ctx := t.Context()

	dlv, err := delivery.New(delivery.NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        map[string]string{"name": "X"},
		Domain:        domain,
		ScheduledTime: time.Now().UTC().Add(-time.Minute),
		Backoff:       delivery.DefaultBackoff,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Schedule(ctx, dlv))

	_, err = claimer.ClaimForValidation(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, sqlc.SendingPoolStatusValidating, poolRow(t, batchID, email).Status,
		"fixture precondition: the delivery must be claimed for validation")
}

// backdateClaim rewrites claimed_at to `age` ago, standing in for a claim taken
// that long before now.
func backdateClaim(t *testing.T, batchID batch.ID, email string, age time.Duration) {
	t.Helper()
	_, err := db.Exec(t.Context(),
		`UPDATE sending_pool_emails
		    SET claimed_at = NOW() - make_interval(secs => $1)
		  WHERE message_id = $2 AND email = $3`,
		age.Seconds(), batchID.String(), email)
	require.NoError(t, err)
}

func poolRow(t *testing.T, batchID batch.ID, email string) sqlc.SendingPoolEmail {
	t.Helper()
	row, err := q.GetPool(t.Context(), sqlc.GetPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
	require.NoError(t, err)
	return row
}
