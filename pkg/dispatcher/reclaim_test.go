package dispatcher

// Reclaiming Deliveries stranded in 'sending' — the half of ADR 0004 that was
// never built (#378, ADR 0007).
//
// These tests drive the REAL ReclaimCycle + REAL pool.Claimer + REAL Postgres
// (dockertest, via the TestMain in dispatch_cycle_incident_test.go). They own
// their fixture rows and delete them again, because the pool is shared with
// every other test in this package.

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

// TestReclaimCycle_SendingStrandedPastThreshold_ReturnsToPoolWithAttemptBumped
// pins the recovery itself. Before this existed, a Delivery whose claim was
// lost — the worker died mid-send, or the outcome stat never reached the
// Dispatcher (ADR 0004's accepted "rare stranded Delivery") — sat in 'sending'
// forever: the only exits from that status are stats-driven, and no stat was
// ever coming. Nothing retried it and the sender was never told.
//
// The attempt counter must be bumped: it is what makes a condition that
// strands systematically converge on termination through the Retry Budget
// instead of looping for ever.
func TestReclaimCycle_SendingStrandedPastThreshold_ReturnsToPoolWithAttemptBumped(t *testing.T) {
	ctx := t.Context()
	batchID, domain := seedReclaimBatch(t)

	repo := sqlc.NewDeliveryRepository(testDB, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	claimer := pool.NewClaimer(repo)
	d := &disp{claimer: claimer}

	email := "stranded@" + domain
	claimForDispatch(t, repo, claimer, batchID, domain, email)

	// The claim is older than sendingStrandThreshold: whatever was going to
	// happen to this Envelope has happened by now.
	backdateClaim(t, batchID, email, sendingStrandThreshold+time.Minute)

	require.NoError(t, d.ReclaimCycle(ctx))

	got := poolRow(t, batchID, email)
	assert.Equal(t, sqlc.SendingPoolStatusScheduled, got.Status,
		"a Delivery stranded in 'sending' past the threshold must be handed back to the pool")
	assert.EqualValues(t, 1, got.SendAttemptsCnt,
		"a reclaim from 'sending' counts as a spent attempt, so the Retry Budget can eventually end the loop")
	assert.False(t, got.ClaimedAt.Valid,
		"a Delivery that is no longer in flight must not claim to be: claimed_at is cleared")
}

// TestReclaimCycle_FreshlyClaimedUnderBacklog_IsUntouched is the assertion this
// whole part exists for. #378 proposed reclaiming on
// `status='sending' AND scheduled_time < NOW() - INTERVAL '15 minutes'`, but
// PrepareForSend never touches scheduled_time: under a backlog — exactly the
// condition in which a reclaim earns its keep — that column is hours in the past
// on a row claimed one second ago. Such a predicate resets live sends, and
// because the send guard is keyed on the stream sequence of the message being
// delivered (ADR 0004), every one of them is published afresh and delivered a
// second time. The threshold must be measured from claimed_at and nothing else.
func TestReclaimCycle_FreshlyClaimedUnderBacklog_IsUntouched(t *testing.T) {
	ctx := t.Context()
	batchID, domain := seedReclaimBatch(t)

	repo := sqlc.NewDeliveryRepository(testDB, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	claimer := pool.NewClaimer(repo)
	d := &disp{claimer: claimer}

	email := "inflight@" + domain
	claimForDispatch(t, repo, claimer, batchID, domain, email)

	// A backlog: this Delivery was due six hours ago and is being sent right
	// now. Both facts are true at once, and only the second one matters.
	backdateSchedule(t, batchID, email, 6*time.Hour)
	backdateClaim(t, batchID, email, 1*time.Second)

	require.NoError(t, d.ReclaimCycle(ctx))

	got := poolRow(t, batchID, email)
	assert.Equal(t, sqlc.SendingPoolStatusSending, got.Status,
		"a Delivery claimed one second ago is in flight, however long ago it was scheduled")
	assert.EqualValues(t, 0, got.SendAttemptsCnt,
		"a live send must not have its attempt counter bumped under it")
	assert.True(t, got.ClaimedAt.Valid, "a live claim must be left alone")
}

// seedReclaimBatch seeds the Batch row that sending_pool_emails' foreign key
// needs, plus the Domain that owns it and the Template it names, and removes
// them afterwards.
//
// The Domain's DKIM key is deliberately not a key: nothing here builds an
// Envelope except the Retry Budget tests, which want a Delivery that cannot be
// built.
func seedReclaimBatch(t *testing.T) (batch.ID, string) {
	t.Helper()
	ctx := t.Context()
	q := sqlc.New(testDB)

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
	require.NoError(t, sqlc.NewBatchRepository(testDB).Create(ctx, b))

	// context.Background(), not t.Context(): the test's context is cancelled
	// before Cleanup runs, and a fixture row left behind here would show up in
	// the pool-wide counts other tests in this package assert on.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck // best-effort test cleanup
		testDB.Exec(cleanupCtx, "DELETE FROM sending_pool_emails WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		testDB.Exec(cleanupCtx, "DELETE FROM messages WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		testDB.Exec(cleanupCtx, "DELETE FROM templates WHERE domain = $1", domain)
		//nolint:errcheck // best-effort test cleanup
		testDB.Exec(cleanupCtx, "DELETE FROM domains WHERE domain = $1", domain)
	})

	return b.ID(), domain
}

// claimForDispatch takes one Delivery all the way through the real claim path
// — scheduled, validated, then claimed into 'sending' — so the row under test
// carries exactly the state a live dispatch leaves behind.
func claimForDispatch(t *testing.T, repo delivery.Repository, claimer pool.Claimer, batchID batch.ID, domain, email string) {
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
	require.NoError(t, claimer.MarkValidated(ctx, dlv))

	_, err = claimer.ClaimForDispatch(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, sqlc.SendingPoolStatusSending, poolRow(t, batchID, email).Status,
		"fixture precondition: the delivery must be claimed for dispatch")
}

// backdateClaim rewrites claimed_at to `age` ago, standing in for a claim
// taken that long before now.
func backdateClaim(t *testing.T, batchID batch.ID, email string, age time.Duration) {
	t.Helper()
	_, err := testDB.Exec(t.Context(),
		`UPDATE sending_pool_emails
		    SET claimed_at = NOW() - make_interval(secs => $1)
		  WHERE message_id = $2 AND email = $3`,
		age.Seconds(), batchID.String(), email)
	require.NoError(t, err)
}

// backdateSchedule rewrites scheduled_time to `age` ago, standing in for the
// backlog the claim path never clears.
func backdateSchedule(t *testing.T, batchID batch.ID, email string, age time.Duration) {
	t.Helper()
	_, err := testDB.Exec(t.Context(),
		`UPDATE sending_pool_emails
		    SET scheduled_time = NOW() - make_interval(secs => $1),
		        original_scheduled_time = NOW() - make_interval(secs => $1)
		  WHERE message_id = $2 AND email = $3`,
		age.Seconds(), batchID.String(), email)
	require.NoError(t, err)
}

func poolRow(t *testing.T, batchID batch.ID, email string) sqlc.SendingPoolEmail {
	t.Helper()
	row, err := sqlc.New(testDB).GetPool(t.Context(), sqlc.GetPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
	require.NoError(t, err)
	return row
}
