package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/internal/tests"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
)

// What an operator reads out of the audit_records table, spelled here as the literals they are.
// Deliberately not imported from the code that writes them: these values are the register's contract
// with whoever queries it, and a test that took them from the producer would keep passing on the day
// one of them silently changed spelling.
const (
	// The Principal of every administrative operation, and the whole of what a shared Admin Token
	// can say: a holder acted, never which one (ADR 0009 owes per-operator credentials).
	adminTokenPrincipal = "admin-token"

	outcomeAllowed = "allowed"
	outcomeDenied  = "denied"

	actionCreate = "create"
	actionUpdate = "update"
)

// auditRecord is one row of audit_records as an operator would read it back — the columns, plus the
// three things worth asserting out of the jsonb payload.
type auditRecord struct {
	principal   string
	resource    []string
	action      string
	outcome     string
	occurredAt  time.Time
	attribution string
	reason      string
	grants      int
}

// auditDB opens a handle for reading the register back. A raw query and not a repository read: ADR
// 0012 gives the register no read path at all — no API, no query surface, and a Repository with no
// method that could return a Record — so that no authorization decision can ever be influenced by
// what the register says about the decisions before it. SQL is what is left, and is the right tool.
func auditDB(t *testing.T, infra *TestInfrastructure) *pgxpool.Pool {
	t.Helper()
	db, err := pgxpool.New(t.Context(), infra.dbURL)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

// auditRecordsOf reads the Audit Records whose Resource names one Domain and keeps the ones matching.
//
// The filter is on the second segment of the resource array, which is the query the segments exist to
// make possible: it cannot match a differently-named Domain, where a prefix over a joined path would
// also match <name>.evil.com and hand a reader another tenant's records (ADR 0010). It is also what
// isolates these subtests from one another — the suite runs them in parallel against one Kannon, and
// each gets a Domain of its own, so filtering by the Domain is exact where filtering by time or by
// the shared Principal would pick up whatever else was running.
func auditRecordsOf(ctx context.Context, db *pgxpool.Pool, domain string, matches func(auditRecord) bool) ([]auditRecord, error) {
	rows, err := db.Query(ctx, `
		SELECT principal, resource, action, outcome, occurred_at,
		       coalesce(data->>'attribution', ''),
		       coalesce(data->>'reason', ''),
		       coalesce(jsonb_array_length(data->'grants'), 0)
		  FROM audit_records
		 WHERE resource[2] = $1`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var kept []auditRecord
	for rows.Next() {
		var r auditRecord
		if err := rows.Scan(&r.principal, &r.resource, &r.action, &r.outcome, &r.occurredAt,
			&r.attribution, &r.reason, &r.grants); err != nil {
			return nil, err
		}
		if matches(r) {
			kept = append(kept, r)
		}
	}
	return kept, rows.Err()
}

// requireAuditRecord waits until exactly one Audit Record naming domain satisfies matches, and
// returns it. It has to wait: a decision is published fire-and-forget and written by a worker of its
// own, so nothing about the register is true the moment the call that caused it returns. Same
// EventuallyWithT shape and same timeout scale as the suite's waits for a stat row to land.
func requireAuditRecord(t *testing.T, db *pgxpool.Pool, domain string, matches func(auditRecord) bool) auditRecord {
	t.Helper()

	var found auditRecord
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		got, err := auditRecordsOf(t.Context(), db, domain, matches)
		require.NoError(tt, err)
		require.Len(tt, got, 1)
		found = got[0]
	}, 30*time.Second, 500*time.Millisecond,
		"expected exactly one Audit Record naming %s", domain)

	return found
}

// onResource matches a Record by the exact path it names, for the assertions that would otherwise
// also pick up the API Key creation every fresh Domain comes with — that being create too.
func onResource(segments ...string) func(auditRecord) bool {
	return func(r auditRecord) bool {
		return slices.Equal(r.resource, segments)
	}
}

// isUpdate matches a Record by its Action. Enough on its own for a Domain the suite has only just
// created: update names the Domain itself, and nothing else done to a fresh one asks for it.
func isUpdate(r auditRecord) bool {
	return r.action == actionUpdate
}

// testPermittedAdminOperationIsRecorded is the register at its plainest, and the question #443 opens
// with: an operator changed something, and afterwards the table says which credential changed what.
// Without it an operator is back to answering "what did this credential do last month" out of debug
// logs that a production deployment does not have enabled.
func testPermittedAdminOperationIsRecorded(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	db := auditDB(t, infra)
	client := clientFactory.NewClient(t, infra)

	// The window the call itself occupied. occurred_at has to fall inside it, and that is what says
	// the instant is the decision's rather than the writer's: a Record crosses NATS and a worker
	// before it becomes a row, so a value stamped when the row was written would land after `after`.
	before := time.Now()
	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
	})
	after := time.Now()

	rec := requireAuditRecord(t, db, client.domain, isUpdate)

	assert.Equal(t, adminTokenPrincipal, rec.principal,
		"an administrative act is recorded against the credential Kannon authenticated")
	// Setting a Tracking Policy is update on the Domain itself, not on anything beneath it (ADR 0008).
	assert.Equal(t, []string{"domains", client.domain}, rec.resource)
	assert.Equal(t, outcomeAllowed, rec.outcome)
	// A millisecond of slack each way, timestamptz having microsecond resolution where a Go instant
	// has nanoseconds.
	assert.WithinRange(t, rec.occurredAt, before.Add(-time.Millisecond), after.Add(time.Millisecond),
		"the instant recorded is the decision's, not the moment a worker got round to writing it")
}

// testRefusedOperationIsRecordedAsDenied is the half of the register an operator consults after an
// incident rather than during an audit: what has been refused, and what was it reaching for. A
// refusal that left no row would make "has anything been denied on this Domain" unanswerable, which
// is the state #443 found the system in.
func testRefusedOperationIsRecordedAsDenied(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	db := auditDB(t, infra)
	client := clientFactory.NewClient(t, infra)

	// An API Key resolves to sender on its own Domain and nowhere else, so a send whose From is at an
	// unrelated Domain asks for create on *that* Domain's Batches and is refused. Unrelated and not a
	// parent, deliberately: a From host that is a proper parent of the tenant is authorized against
	// the tenant's own Batches instead (ADR 0008), and such a send would be permitted.
	unrelated := tests.FakeDomain(t)

	err := client.SendEmailExpectingFailure(t, &mailerapiv1.SendHTMLReq{
		Sender:        &mailertypes.Sender{Email: "sender@" + unrelated, Alias: defaultSenderAlias},
		Recipients:    []*mailertypes.Recipient{{Email: tests.FakeEmail(t)}},
		Subject:       "Audit Denied Send Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	})
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Filed under the Domain the send reached for and not the tenant it came from, which is what
	// makes "what has been refused on this Domain" a question the register answers.
	rec := requireAuditRecord(t, db, unrelated, onResource("domains", unrelated, "batches"))

	assert.Equal(t, outcomeDenied, rec.outcome)
	assert.Equal(t, actionCreate, rec.action)
	assert.NotEmpty(t, rec.reason,
		"'denied' says that a request was refused and never why, which is the half a reader needs")
	assert.Positive(t, rec.grants,
		"a security reviewer reads a refusal for the authority the Principal actually held")
}

// testAttributionIsRecordedBesideTheCredential is ADR 0008's deferral discharged: a front-end holding
// the operator's token names one of its own users, and the claim survives into a durable record
// instead of a log line the integrator does not control. Both must be in the same row, and told
// apart — one of the two was checked and the other cannot be, so a record that folded the claim into
// the principal column would read as though Kannon knew the person it names.
func testAttributionIsRecordedBesideTheCredential(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	db := auditDB(t, infra)
	client := clientFactory.NewClient(t, infra)

	const attribution = "alice@corp.example"

	// The same credential as every other Admin API call in the suite, plus a claim about who asked
	// for this one. Two sets of options and not one: the token says what may be done, the claim says
	// on whose behalf, and the header carrying it confers nothing at all (ADR 0009).
	opts := authzconnect.AdminTokenClientOptions(adminToken)
	opts = append(opts, authzconnect.AttributionClientOptions(attribution)...)
	attributed := adminv1connect.NewApiClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
		opts...,
	)

	_, err := attributed.SetTrackingPolicy(t.Context(), connect.NewRequest(&adminapiv1.SetTrackingPolicyReq{
		Domain: client.domain,
		Tracking: &trackingtypes.TrackingPolicy{
			Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
			Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		},
	}))
	require.NoError(t, err, "an Attribution must not change what the credential may do")

	rec := requireAuditRecord(t, db, client.domain, isUpdate)

	assert.Equal(t, adminTokenPrincipal, rec.principal,
		"the credential is what was authenticated, and the record names it")
	assert.Equal(t, attribution, rec.attribution,
		"the claim is what was asserted, and the record holds it beside the credential")
	assert.Equal(t, outcomeAllowed, rec.outcome)
}

// testMailerSendIsRecordedOncePerBatch is why the Recorder is installed once over the whole API and
// not on the surfaces the Admin Token authenticates: the Mailer API authenticates its own API Key,
// so anything hung off the admin options would have missed every send. If this broke, the register
// would hold every administrative act and no mail — which nobody would notice until they asked.
func testMailerSendIsRecordedOncePerBatch(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	db := auditDB(t, infra)
	client := clientFactory.NewClient(t, infra)

	client.SendEmail(t, &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: tests.FakeEmail(t)},
			{Email: tests.FakeEmail(t)},
			{Email: tests.FakeEmail(t)},
		},
		Subject:       "Audit Mailer Send Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	})

	onBatches := onResource("domains", client.domain, "batches")
	rec := requireAuditRecord(t, db, client.domain, onBatches)

	assert.Equal(t, actionCreate, rec.action, "sending is create on the Domain's Batches (ADR 0008)")
	assert.Equal(t, outcomeAllowed, rec.outcome)
	assert.NotEqual(t, adminTokenPrincipal, rec.principal,
		"a send authenticates with the Domain's own API Key and never with the operator's token")
	assert.Contains(t, rec.principal, "@"+client.domain,
		"the record names the API Key that acted, which is what makes a send traceable to a key")

	// Three Recipients and one row: the volume of this register is the API call rate and not the mail
	// volume, a send being one decision about the Batch. Held over a few polls rather than asserted
	// once, since a per-Recipient record would arrive just after the first and pass the wait above.
	// Polled by hand rather than with require.Never, whose goroutine outlives the subtest.
	for range 6 {
		got, err := auditRecordsOf(t.Context(), db, client.domain, onBatches)
		require.NoError(t, err)
		require.Len(t, got, 1, "one Audit Record per Batch, never one per Recipient")
		time.Sleep(500 * time.Millisecond)
	}
}
