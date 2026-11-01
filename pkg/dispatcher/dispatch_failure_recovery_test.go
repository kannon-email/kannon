package dispatcher

// Dispatch-failure recovery (#400) with a genuine failure: the Batch names a
// Template that does not exist yet, so the real Builder cannot build its
// Envelope. The claimed Delivery must go back to the Pool with its attempt
// counter bumped, and once the Template is created the next cycle must publish
// it. dispatch_cycle_incident_test.go covers the same recovery path with the
// failure faked by a deadline.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/kannon-email/kannon/internal/statssec"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

func TestDispatchCycle_MissingTemplate_ReschedulesThenPublishesOnceCreated(t *testing.T) {
	ctx := t.Context()
	q := sqlc.New(testDB)
	batchID, domain, templateID := seedBatchWithMissingTemplate(t)
	email := "recovery@" + domain

	// Collapsed backoff, so the rescheduled Delivery is due again at once; the
	// production Retry Budget is far from spent after one attempt.
	backoff := delivery.ExponentialBackoff{Base: time.Millisecond, Min: time.Millisecond}
	repo := sqlc.NewDeliveryRepository(testDB, backoff, delivery.DefaultRetryWindow)
	claimer := pool.NewClaimer(repo)
	pub := &subjectPublisher{}
	d := &disp{
		claimer: claimer,
		eb:      envelope.NewBuilder(sqlc.NewSendingDataSource(testDB), statssec.NewStatsService(statssec.NewInMemKeyRepository()), returnpath.MustParse(strings.Repeat("s", returnpath.MinSecretLength)), 0),
		pub:     pub,
	}

	scheduleValidated(t, repo, claimer, batchID, domain, email)

	require.NoError(t, d.DispatchCycle(ctx))

	got := poolRow(t, batchID, email)
	assert.Equal(t, sqlc.SendingPoolStatusScheduled, got.Status,
		"a Delivery whose Envelope cannot be built must be handed back to the pool, not stranded in 'sending'")
	assert.EqualValues(t, 1, got.SendAttemptsCnt, "the failed dispatch must bump the attempt counter")
	assert.Empty(t, pub.sent, "nothing may be published while the Template is missing")

	_, err := q.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: templateID,
		Html:       "<html><body><h1>Recovered</h1></body></html>",
		Domain:     domain,
		Type:       sqlc.TemplateTypeTransient,
	})
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.NoError(c, d.DispatchCycle(ctx))
		assert.Len(c, envelopesTo(c, pub, email), 1)
	}, 5*time.Second, 20*time.Millisecond, "the Delivery must be published once its Template exists")

	assert.Equal(t, sqlc.SendingPoolStatusSending, poolRow(t, batchID, email).Status,
		"the published Delivery awaits the sender's feedback")
	assert.Empty(t, pub.statsOn(t, "kannon.stats.failed"), "a recovered Delivery must not be reported as Failed")
}

// seedBatchWithMissingTemplate seeds a Domain with a usable DKIM key and a
// Batch whose Template has not been created, and removes them afterwards.
func seedBatchWithMissingTemplate(t *testing.T) (batch.ID, string, string) {
	t.Helper()
	ctx := t.Context()
	q := sqlc.New(testDB)

	keys, err := dkim.GenerateDKIMKeysPair()
	require.NoError(t, err)

	domain := fmt.Sprintf("recovery-%d.test", time.Now().UnixNano())
	_, err = q.CreateDomain(ctx, sqlc.CreateDomainParams{
		Domain:         domain,
		DkimPrivateKey: keys.PrivateKey,
		DkimPublicKey:  keys.PublicKey,
	})
	require.NoError(t, err)

	templateID := "tpl_recovery@" + domain
	b, err := batch.New(batch.NewParams{
		Domain:      domain,
		Subject:     "recovery",
		Sender:      batch.Sender{Email: "noreply@" + domain, Alias: "Recovery"},
		TemplateID:  templateID,
		Attachments: batch.Attachments{},
	})
	require.NoError(t, err)
	require.NoError(t, sqlc.NewBatchRepository(testDB).Create(ctx, b))

	// context.Background(), not t.Context(): the test's context is cancelled
	// before Cleanup runs, and leftover rows would skew the pool-wide counts
	// other tests in this package assert on.
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

	return b.ID(), domain, templateID
}

func envelopesTo(c *assert.CollectT, p *subjectPublisher, email string) []*mailertypes.EmailToSend {
	var out []*mailertypes.EmailToSend
	for _, m := range p.sent {
		if m.subject != "kannon.sending" {
			continue
		}
		e := &mailertypes.EmailToSend{}
		require.NoError(c, proto.Unmarshal(m.data, e))
		if e.To == email {
			out = append(out, e)
		}
	}
	return out
}
