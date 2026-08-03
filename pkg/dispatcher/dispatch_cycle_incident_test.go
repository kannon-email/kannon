package dispatcher

// Incident regression — dispatcher budget death mid-page (#400).
//
// In production a single multi-recipient Batch saw a large tail of its
// Deliveries fail inside eb.Build with err="context deadline exceeded",
// all within a few milliseconds, and remain stranded in status='sending'
// forever (no reclaim, no retry): the per-cycle context budget was SHARED
// across the whole claimed page, and a claimed-but-failed Delivery had no
// recovery path at all.
//
// This test drives the REAL DispatchCycle + REAL pool.Claimer + REAL
// Postgres (dockertest) through the same mechanism and asserts the fixed
// behaviour:
//
//  1. a Delivery that exhausts its per-Delivery budget fails ALONE — it
//     cannot guillotine the rest of the claimed page;
//  2. every claimed-but-failed Delivery is handed back to the pool
//     (status='scheduled', attempt counter bumped) and is eventually
//     published by later healthy cycles — zero silent loss.
//
// The DB latency spike is injected at the seam the builder already exposes
// (envelope.NewBuilderWith): gateSource serves the first `free` calls
// instantly, then blocks until its context dies.

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

var testDB *pgxpool.Pool

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	testDB, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("could not start test postgres", "err", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := purge(); err != nil {
		slog.Error("could not purge test postgres", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// gateSource simulates the production DB latency spike inside Build.
// The first `free` GetSendingData calls return instantly; the next call
// blocks until its context dies; every call after that sees a dead parent
// and fails in microseconds — the original incident burst.
type gateSource struct {
	data  envelope.SendingData
	free  int
	calls int
}

func (s *gateSource) GetSendingData(ctx context.Context, _ batch.ID) (envelope.SendingData, error) {
	s.calls++
	if s.calls <= s.free {
		return s.data, nil
	}
	<-ctx.Done()
	return envelope.SendingData{}, ctx.Err()
}

type noopTokens struct{}

func (noopTokens) CreateLinkToken(_ context.Context, _, _, _ string, _ tracking.Mode) (string, error) {
	return "tok", nil
}

func (noopTokens) CreateOpenToken(_ context.Context, _, _ string, _ tracking.Mode) (string, error) {
	return "tok", nil
}

// recordingPublisher captures the recipient of every published Envelope.
type recordingPublisher struct {
	published []string
}

func (p *recordingPublisher) Publish(_ string, data []byte) error {
	var m mailertypes.EmailToSend
	if err := proto.Unmarshal(data, &m); err != nil {
		return err
	}
	p.published = append(p.published, m.To)
	return nil
}

func TestDispatchCycle_BudgetDeathMidPage_NoDeliveryLost(t *testing.T) {
	ctx := t.Context()
	q := sqlc.New(testDB)

	// --- seed: Domain + transient Template + Batch (same shape as a
	// SendTemplate call) --------------------------------------------------
	keys, err := dkim.GenerateDKIMKeysPair()
	require.NoError(t, err)

	const domain = "k.incident.test"
	_, err = q.CreateDomain(ctx, sqlc.CreateDomainParams{
		Domain:         domain,
		DkimPrivateKey: keys.PrivateKey,
		DkimPublicKey:  keys.PublicKey,
	})
	require.NoError(t, err)

	tpl, err := q.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: "template_incident@" + domain,
		Html:       `<html><body><p style="font-size:18px">test</p></body></html>`,
		Domain:     domain,
		Type:       sqlc.TemplateTypeTransient,
	})
	require.NoError(t, err)

	b, err := batch.New(batch.NewParams{
		Domain:      domain,
		Subject:     "incident repro",
		Sender:      batch.Sender{Email: "noreply@" + domain, Alias: "Incident"},
		TemplateID:  tpl.TemplateID,
		Attachments: batch.Attachments{},
	})
	require.NoError(t, err)
	require.NoError(t, sqlc.NewBatchRepository(testDB).Create(ctx, b))

	// --- seed: 50 due Deliveries, validated ('scheduled') ----------------
	const (
		total    = 50
		page     = 20 // the hardcoded claim cap in DispatchCycle
		accepted = 3  // builds that succeed before the budget dies
	)
	// Production backoff floors retries at 5m; collapse it so rescheduled
	// Deliveries become due again within test time (the e2e suite does the
	// same via container.WithBackoff).
	backoff := delivery.ExponentialBackoff{Base: 10 * time.Millisecond, Min: 10 * time.Millisecond}
	// The production Retry Budget against a collapsed curve is effectively
	// unreachable (10ms·2ⁿ passes 24h only around the 23rd attempt), which is
	// what this test wants: it asserts that every rescheduled victim is
	// eventually published, so nothing here may be terminated. The budget's own
	// boundary is exercised in retry_budget_test.go.
	repo := sqlc.NewDeliveryRepository(testDB, backoff, delivery.DefaultRetryWindow)
	claimer := pool.NewClaimer(repo)

	ds := make([]*delivery.Delivery, total)
	for i := range ds {
		d, err := delivery.New(delivery.NewParams{
			BatchID:       b.ID(),
			Email:         fmt.Sprintf("victim%02d@%s", i, domain),
			Fields:        map[string]string{"name": "X"},
			Domain:        domain,
			ScheduledTime: time.Now().UTC().Add(-time.Minute),
			Backoff:       backoff,
		})
		require.NoError(t, err)
		ds[i] = d
	}
	require.NoError(t, repo.Schedule(ctx, ds...))
	for _, d := range ds {
		require.NoError(t, claimer.MarkValidated(ctx, d))
	}

	// --- the real dispatcher, with latency injected in Build -------------
	src := &gateSource{
		free: accepted,
		data: envelope.SendingData{
			Subject:        "incident repro",
			HTML:           tpl.Html,
			Domain:         domain,
			MessageID:      b.ID().String(),
			SenderEmail:    "noreply@" + domain,
			SenderAlias:    "Incident",
			DkimPrivateKey: keys.PrivateKey,
		},
	}
	pub := &recordingPublisher{}
	d := &disp{
		claimer: claimer,
		eb:      envelope.NewBuilderWith(src, noopTokens{}),
		pub:     pub,
	}

	// Cycle 1 — the incident cycle. The parent context carries a 1s
	// deadline, shrinking the per-Delivery budget into test time; gateSource
	// blocks build #accepted+1 until that deadline fires, and every later
	// build in the page sees a dead parent.
	cycleCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	start := time.Now()
	require.NoError(t, d.DispatchCycle(cycleCtx),
		"per-email failures are handled in-loop: the cycle itself reports success")
	elapsed := time.Since(start)

	// The claim cap held: one page of 20, despite 50 due.
	assert.Len(t, pub.published, accepted,
		"only the builds that ran before the budget death are published")

	// FIX assertion 1 — no Delivery is stranded in 'sending': the failed
	// tail of the page (page-accepted rows) is back in 'scheduled' with the
	// attempt counter bumped, ready to be claimed again. Only the published
	// ones legitimately remain 'sending' (waiting for SMTPSender feedback,
	// absent in this harness).
	assert.Equal(t, accepted, countByStatus(t, "sending"),
		"only published deliveries may sit in 'sending'")
	assert.Equal(t, total-accepted, countByStatus(t, "scheduled"),
		"claimed-but-failed deliveries must be handed back to the pool")
	assert.Equal(t, page-accepted, countRescheduled(t),
		"every failed delivery of the claimed page carries one bumped attempt")

	// The failure burst still ends quickly (failed builds don't run
	// serially into their own 5s budgets once the parent is dead)...
	assert.Less(t, elapsed, 3*time.Second,
		"after the deadline fires, remaining builds must fail fast")

	// --- cycles 2..n: latency gone, drain the pool -----------------------
	src.free = math.MaxInt // heal the "DB": every later build succeeds

	// FIX assertion 2 — zero silent loss: every Delivery, including the
	// rescheduled victims, is eventually published by healthy cycles.
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		require.NoError(tt, d.DispatchCycle(ctx))
		assert.Len(tt, pub.published, total)
	}, 10*time.Second, 20*time.Millisecond,
		"healthy cycles must drain the backlog, rescheduled victims included")

	assert.Equal(t, 0, countByStatus(t, "scheduled"), "no delivery left behind")
	assert.Equal(t, total, countByStatus(t, "sending"),
		"all deliveries were published and await stats feedback")

	published := make(map[string]bool, len(pub.published))
	for _, to := range pub.published {
		published[to] = true
	}
	for _, dlv := range ds {
		assert.True(t, published[dlv.Email()], "delivery %s was never published", dlv.Email())
	}
}

func countByStatus(t *testing.T, status string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(t.Context(),
		`SELECT count(*) FROM sending_pool_emails WHERE status = $1`, status).Scan(&n))
	return n
}

func countRescheduled(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(t.Context(),
		`SELECT count(*) FROM sending_pool_emails WHERE send_attempts_cnt > 0`).Scan(&n))
	return n
}
