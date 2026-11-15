package e2e_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/tests"
)

var auditScenarios = map[string]func(*testing.T, *env){
	"PermittedAdminOperationIsRecorded":        testPermittedAdminOperationIsRecorded,
	"RefusedOperationIsRecordedAsDenied":       testRefusedOperationIsRecordedAsDenied,
	"AttributionIsRecordedBesideTheCredential": testAttributionIsRecordedBesideTheCredential,
	"SendIsRecordedOncePerBatch":               testSendIsRecordedOncePerBatch,
}

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

// auditDB opens a handle for reading the register back. This is the e2e suite's single permitted
// reach past Kannon's public surfaces (CODING_STANDARDS.md): Audit Records have no read API until
// #515.
func auditDB(t *testing.T, e *env) *pgxpool.Pool {
	t.Helper()
	db, err := pgxpool.New(t.Context(), e.dbURL)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

// auditRecordsOf reads the Audit Records whose Resource names one Domain and keeps the ones matching.
//
// The filter is on the second segment of the resource array: it cannot match a differently-named
// Domain, where a prefix over a joined path would also match <name>.evil.com (ADR 0010). It is also
// what isolates parallel scenarios, each of which has a Domain of its own.
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

// awaitAuditRecord waits until exactly one Audit Record naming domain satisfies matches. A decision
// is published fire-and-forget and written by a worker of its own, so nothing about the register is
// true the moment the call that caused it returns.
func awaitAuditRecord(t *testing.T, db *pgxpool.Pool, domain string, matches func(auditRecord) bool) auditRecord {
	t.Helper()
	var found auditRecord
	eventually(t, "exactly one matching Audit Record naming "+domain, func(c *probe) {
		got, err := auditRecordsOf(t.Context(), db, domain, matches)
		require.NoError(c, err)
		require.Len(c, got, 1)
		found = got[0]
	})
	return found
}

// onResource matches a Record by the exact path it names.
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

// testPermittedAdminOperationIsRecorded: an operator changed something, and afterwards the register
// says which credential changed what, and when the decision was taken.
func testPermittedAdminOperationIsRecorded(t *testing.T, e *env) {
	db := auditDB(t, e)
	tn := e.NewTenant(t)

	before := time.Now()
	tn.SetTracking(t, restclient.Identified, restclient.Identified)
	after := time.Now()

	rec := awaitAuditRecord(t, db, tn.Domain, isUpdate)

	assert.Equal(t, adminTokenPrincipal, rec.principal,
		"an administrative act is recorded against the credential Kannon authenticated")
	assert.Equal(t, []string{"domains", tn.Domain}, rec.resource,
		"setting a Tracking Policy is update on the Domain itself (ADR 0008)")
	assert.Equal(t, outcomeAllowed, rec.outcome)
	// A millisecond of slack each way, timestamptz having microsecond resolution where a Go instant
	// has nanoseconds.
	assert.WithinRange(t, rec.occurredAt, before.Add(-time.Millisecond), after.Add(time.Millisecond),
		"the instant recorded is the decision's, not the moment a worker got round to writing it")
}

// testRefusedOperationIsRecordedAsDenied: what has been refused, and what was it reaching for.
func testRefusedOperationIsRecordedAsDenied(t *testing.T, e *env) {
	db := auditDB(t, e)
	tn := e.NewTenant(t)

	// A sender under an unrelated Domain violates the intake constraint even though the key may
	// create Batches under its own Domain.
	batch := tn.Batch("Audit Denied Send Test", "<h1>Hello!</h1>", To(tests.FakeEmail(t), nil))
	batch.From.Email = openapi_types.Email("sender@" + tests.FakeDomain(t))
	res := tn.TrySend(t, batch)
	assert.Equal(t, http.StatusForbidden, res.StatusCode(), string(res.Body))

	rec := awaitAuditRecord(t, db, tn.Domain, onResource("domains", tn.Domain, "batches"))

	assert.Equal(t, outcomeDenied, rec.outcome)
	assert.Equal(t, actionCreate, rec.action)
	assert.NotEmpty(t, rec.reason,
		"'denied' says that a request was refused and never why, which is the half a reader needs")
	assert.Positive(t, rec.grants,
		"a security reviewer reads a refusal for the authority the Principal actually held")
}

// testAttributionIsRecordedBesideTheCredential: a front-end holding the operator's token names one of
// its own users on the Attribution header (ADR 0009), and the claim survives into the record beside
// the credential, never folded into it (ADR 0010).
func testAttributionIsRecordedBesideTheCredential(t *testing.T, e *env) {
	db := auditDB(t, e)
	tn := e.NewTenant(t)

	const attribution = "alice@corp.example"
	claim := attribution
	tn.setTracking(t, e.admin, &restclient.UpdateDomainParams{ParameterAttribution: &claim},
		restclient.Identified, restclient.Identified)

	rec := awaitAuditRecord(t, db, tn.Domain, isUpdate)

	assert.Equal(t, adminTokenPrincipal, rec.principal,
		"the credential is what was authenticated, and the record names it")
	assert.Equal(t, attribution, rec.attribution,
		"the claim is what was asserted, and the record holds it beside the credential")
	assert.Equal(t, outcomeAllowed, rec.outcome)
}

// testSendIsRecordedOncePerBatch: a send authenticates with the Domain's own API Key, is recorded
// against that key, and is one decision about the Batch however many Recipients it has.
func testSendIsRecordedOncePerBatch(t *testing.T, e *env) {
	db := auditDB(t, e)
	tn := e.NewTenant(t)

	tn.Send(t, tn.Batch("Audit Send Test", "<h1>Hello!</h1>",
		To(tests.FakeEmail(t), nil), To(tests.FakeEmail(t), nil), To(tests.FakeEmail(t), nil)))

	onBatches := onResource("domains", tn.Domain, "batches")
	rec := awaitAuditRecord(t, db, tn.Domain, onBatches)

	assert.Equal(t, actionCreate, rec.action, "sending is create on the Domain's Batches (ADR 0008)")
	assert.Equal(t, outcomeAllowed, rec.outcome)
	assert.NotEqual(t, adminTokenPrincipal, rec.principal,
		"a send authenticates with the Domain's own API Key and never with the operator's token")
	assert.Contains(t, rec.principal, tn.Domain,
		"the record names the API Key that acted, which is what makes a send traceable to a key")

	tn.Holds(t, "one Audit Record per Batch, never one per Recipient", func(c *probe) {
		got, err := auditRecordsOf(t.Context(), db, tn.Domain, onBatches)
		require.NoError(c, err)
		assert.Len(c, got, 1)
	})
}
