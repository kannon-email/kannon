package dispatcher

// The Retry Budget as the Dispatcher sees it (#378, ADR 0007).
//
// #403 stopped stranding a claimed Delivery whose Envelope could not be built,
// and in doing so opened a loop nothing could close: no Envelope means no
// ShouldRetry flag, so such a Delivery can never bounce, and it was rescheduled
// with a doubling backoff until its next attempt was years away — the sender's
// last stat being `accepted`, permanently. This test drives the REAL
// DispatchCycle + REAL Builder + REAL pool.Claimer + REAL Postgres (dockertest,
// via the TestMain in dispatch_cycle_incident_test.go) through exactly that
// condition and asserts the loop now ends: the Delivery is dropped from the Pool
// and the sender is told, as Failed.
//
// The build failure needs no injection seam. seedReclaimBatch's Batch names a
// Template that was never created, which is the production cause verbatim:
// GetSendingData joins Batch × Template × Domain and there is no foreign key
// from messages.template_id, so deleting a Template orphans every pending
// Delivery of every Batch that referenced it.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/statssec"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
)

// publishedMsg is one NATS publish, kept undecoded. recordingPublisher in
// dispatch_cycle_incident_test.go reads every payload as an EmailToSend, which a
// stats payload is not; these tests need to tell the two subjects apart.
type publishedMsg struct {
	subject string
	data    []byte
}

type subjectPublisher struct {
	sent []publishedMsg
}

func (p *subjectPublisher) Publish(subject string, data []byte) error {
	p.sent = append(p.sent, publishedMsg{subject: subject, data: data})
	return nil
}

// statsOn decodes every payload published on one subject.
func (p *subjectPublisher) statsOn(t *testing.T, subject string) []*statstypes.Stats {
	t.Helper()
	var out []*statstypes.Stats
	for _, m := range p.sent {
		if m.subject != subject {
			continue
		}
		s := &statstypes.Stats{}
		require.NoError(t, proto.Unmarshal(m.data, s))
		out = append(out, s)
	}
	return out
}

// TestDispatchCycle_RetryBudgetSpent_FailsInsteadOfReschedulingForever pins both
// halves of the termination: the Pool row is dropped, and a Failed stat carrying
// a short non-sensitive reason is published for the sender to read.
//
// The budget is shrunk in step with the backoff curve, the way the e2e suite
// does it. Against 100ms·2ⁿ a 250ms window admits the retry at 0 attempts
// (100ms) and at 1 (200ms) and refuses the one at 2 (400ms) — so the Delivery is
// handed back twice and terminated on the third cycle. Nothing waits: a
// reschedule rolls scheduled_time to originalScheduledTime + delay, and the
// fixture's original scheduled time is a minute in the past, so each retry is
// already due.
func TestDispatchCycle_RetryBudgetSpent_FailsInsteadOfReschedulingForever(t *testing.T) {
	ctx := t.Context()
	batchID, domain := seedReclaimBatch(t)
	email := "spent@" + domain

	const retryWindow = 250 * time.Millisecond
	backoff := delivery.ExponentialBackoff{Base: 100 * time.Millisecond, Min: 100 * time.Millisecond}
	require.Less(t, backoff.Delay(1), retryWindow, "fixture arithmetic: the second retry is inside the window")
	require.Greater(t, backoff.Delay(2), retryWindow, "fixture arithmetic: the third retry is outside it")

	repo := sqlc.NewDeliveryRepository(testDB, backoff, retryWindow)
	claimer := pool.NewClaimer(repo)
	q := sqlc.New(testDB)
	pub := &subjectPublisher{}
	d := &disp{
		claimer: claimer,
		eb:      envelope.NewBuilder(q, statssec.NewStatsService(q)),
		pub:     pub,
	}

	scheduleValidated(t, repo, claimer, batchID, domain, email)

	// Cycles 1 and 2 — the budget still has room, so the unbuildable Delivery is
	// handed back to the Pool with its attempt counter bumped, exactly as before.
	for attempt := 1; attempt <= 2; attempt++ {
		require.NoError(t, d.DispatchCycle(ctx))

		got := poolRow(t, batchID, email)
		assert.Equal(t, sqlc.SendingPoolStatusScheduled, got.Status,
			"a Delivery with budget left must be handed back to the pool")
		assert.EqualValues(t, attempt, got.SendAttemptsCnt)
		assert.Empty(t, pub.statsOn(t, "kannon.stats.failed"),
			"a Delivery with budget left must not be reported as Failed")
	}

	// Cycle 3 — the next retry would fall outside the window. This is where the
	// loop used to be endless.
	require.NoError(t, d.DispatchCycle(ctx))

	assert.False(t, poolRowExists(t, batchID, email),
		"a Delivery whose Retry Budget is spent must be dropped from the pool, not rescheduled")

	failed := pub.statsOn(t, "kannon.stats.failed")
	require.Len(t, failed, 1, "exactly one terminal outcome must be published")
	assert.Equal(t, batchID.String(), failed[0].MessageId)
	assert.Equal(t, domain, failed[0].Domain)
	assert.Equal(t, email, failed[0].Email)
	require.NotNil(t, failed[0].Timestamp)

	data := failed[0].Data.GetFailed()
	require.NotNil(t, data, "the stat must carry typed Failed data")
	assert.Equal(t, reasonBudgetSpentDispatching, data.Reason)

	// The reason is customer-visible through the stats API, so it may state what
	// ran out and on which leg and nothing else — no raw error, no address, no
	// database detail.
	assert.NotContains(t, data.Reason, email)
	assert.NotContains(t, data.Reason, domain)

	assert.Empty(t, pub.statsOn(t, "kannon.sending"),
		"no Envelope was ever built, so none may have been published")
}

// TestParseErrors_RetryBudgetSpent_FailsTheDelivery pins the second path into
// the same chokepoint. An error stat is the transient send signal, and the
// Delivery it names is normally rescheduled; when the budget has no room left it
// must be terminated instead, and the returned error must stay nil so the
// consumer acks the stat rather than redelivering it for ever.
func TestParseErrors_RetryBudgetSpent_FailsTheDelivery(t *testing.T) {
	ctx := t.Context()
	batchID, domain := seedReclaimBatch(t)
	email := "spent-on-send@" + domain

	// A window narrower than the very first retry: this Delivery has no room
	// left whatever its attempt count.
	repo := sqlc.NewDeliveryRepository(testDB, delivery.DefaultBackoff, time.Nanosecond)
	claimer := pool.NewClaimer(repo)
	pub := &subjectPublisher{}
	d := &disp{claimer: claimer, pub: pub}

	claimForDispatch(t, repo, claimer, batchID, domain, email)

	require.NoError(t, d.parseErrorsFunc(ctx, &statstypes.Stats{
		MessageId: batchID.String(),
		Domain:    domain,
		Email:     email,
		Data: &statstypes.StatsData{
			Data: &statstypes.StatsData_Error{
				Error: &statstypes.StatsDataError{Code: 421, Msg: "connection reset"},
			},
		},
	}))

	assert.False(t, poolRowExists(t, batchID, email),
		"a Delivery whose Retry Budget is spent must be dropped, not rescheduled")

	failed := pub.statsOn(t, "kannon.stats.failed")
	require.Len(t, failed, 1)
	assert.Equal(t, reasonBudgetSpentSending, failed[0].Data.GetFailed().Reason)
	assert.NotContains(t, failed[0].Data.GetFailed().Reason, "connection reset",
		"the reason must not carry the raw error it was handed")
}

// scheduleValidated puts one Delivery in the Pool ready for the Dispatcher to
// claim itself — scheduled in the past, and validated. Unlike claimForDispatch
// it stops short of the claim, because these tests exercise the real
// DispatchCycle, which claims.
func scheduleValidated(t *testing.T, repo delivery.Repository, claimer pool.Claimer, batchID batch.ID, domain, email string) {
	t.Helper()
	ctx := t.Context()

	dlv, err := delivery.New(delivery.NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        map[string]string{"name": "X"},
		Domain:        domain,
		ScheduledTime: time.Now().UTC().Add(-time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, repo.Schedule(ctx, dlv))
	require.NoError(t, claimer.MarkValidated(ctx, dlv))
}

func poolRowExists(t *testing.T, batchID batch.ID, email string) bool {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(t.Context(),
		`SELECT count(*) FROM sending_pool_emails WHERE message_id = $1 AND email = $2`,
		batchID.String(), email).Scan(&n))
	return n > 0
}
