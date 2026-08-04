package audit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reader hands the specification back what an implementation stored, filtered to one Principal.
//
// Repository is write-only by design: nothing in Kannon reads an Audit Record back, so no
// authorization decision can be influenced by the register describing it (ADR 0010). A specification
// still has to observe what it wrote, so each implementation supplies its own reader instead of the
// interface growing a query that production code would then be holding — the in-memory one reads its
// slice, the sqlc-backed one a query living in its test file and shipping nowhere. Filtering by
// Principal is what lets specifications sharing one Postgres database not see each other's rows.
type Reader func(ctx context.Context, principal string) ([]Record, error)

// RunRepoSpec runs the repository specification against any Repository, reading back through the
// Reader that implementation supplies.
func RunRepoSpec(t *testing.T, repo Repository, read Reader) {
	t.Run("Insert", func(t *testing.T) {
		testInsert(t, repo, read)
	})
	t.Run("Insert/IsIdempotent", func(t *testing.T) {
		testInsertIsIdempotent(t, repo, read)
	})
	t.Run("Insert/StoresTheResourceAsItsSegments", func(t *testing.T) {
		testInsertStoresTheResourceAsItsSegments(t, repo, read)
	})
	t.Run("Insert/RecordsAnAttributionAndTheGrants", func(t *testing.T) {
		testInsertRecordsAnAttributionAndTheGrants(t, repo, read)
	})
	t.Run("Insert/RecordsARequestNothingAuthenticated", func(t *testing.T) {
		testInsertRecordsARequestNothingAuthenticated(t, repo, read)
	})
	t.Run("DeleteOlderThan", func(t *testing.T) {
		testDeleteOlderThan(t, repo, read)
	})
}

// A refusal, because it is the Record with every field filled in: denied is the only outcome that
// carries a Reason, so a permitted operation would leave one field of the payload unasserted.
func testInsert(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()
	principal := isolate("insert")

	written := decisionRecord(principal, time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC))
	written.Action = authz.Delete
	written.Outcome = authz.Denied
	written.Details.Reason = "no grant covers this action on this resource"

	require.NoError(t, repo.Insert(ctx, written))

	stored, err := read(ctx, principal)
	require.NoError(t, err)
	require.Len(t, stored, 1)

	// Record.Equal and not assert.Equal on the struct: a timestamptz comes back in whatever
	// location the connection is in, so two Times naming one instant are not the same value.
	assert.True(t, written.equal(stored[0]), "wrote %+v, read back %+v", written, stored[0])
}

// A redelivery — a Nak after a database error, or a crash between the write and the acknowledgement
// — must not put one decision in the table twice. And two decisions that happen to look alike must
// not be mistaken for one, which is the pair of properties an identifier from the producer buys.
func testInsertIsIdempotent(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()
	principal := isolate("insert-idempotent")
	at := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	redelivered := decisionRecord(principal, at)
	require.NoError(t, repo.Insert(ctx, redelivered))
	require.NoError(t, repo.Insert(ctx, redelivered))

	stored, err := read(ctx, principal)
	require.NoError(t, err)
	require.Len(t, stored, 1, "a redelivered message became a second row")

	// Same Principal, Action, Resource, Outcome and the same instant, under a second identifier:
	// two operations, and two rows. A shared token behind a front-end making two parallel calls is
	// ordinary rather than exotic, so a natural key would collapse a fact that really happened
	// twice — deduplication turns on the identifier and nothing else.
	simultaneous := decisionRecord(principal, at)
	require.NotEqual(t, redelivered.ID, simultaneous.ID)
	require.NoError(t, repo.Insert(ctx, simultaneous))

	stored, err = read(ctx, principal)
	require.NoError(t, err)
	assert.Len(t, stored, 2, "two simultaneous identical operations were stored as one")
}

// The Resource is stored as what the model compares on. A joined path was rejected because a
// Resource's own type calls that form display only, and these two paths are why: one segment holds a
// dot, which is what ruled ltree out, and one holds the separator itself, which a joined form could
// not be read back from unambiguously.
func testInsertStoresTheResourceAsItsSegments(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()
	principal := isolate("insert-resource")
	domain := values.MustParse("sub.example.com")

	wanted := make(map[string][]string)
	for _, resource := range []authz.Resource{
		authz.Domain(domain),
		authz.Template(domain, "welcome/v2.1"),
	} {
		written := decisionRecord(principal, time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC))
		written.Resource = resource.Segments()
		wanted[written.ID] = resource.Segments()
		require.NoError(t, repo.Insert(ctx, written))
	}

	stored, err := read(ctx, principal)
	require.NoError(t, err)
	require.Len(t, stored, len(wanted))

	for _, got := range stored {
		assert.Equal(t, wanted[got.ID], got.Resource, "the segments of %q were not stored as such", got.ID)
	}
}

// What an operator asked "who did this, and what could they have done?" reads. The Attribution is
// the person a caller claimed to be asking for and the one piece of personal data a Record carries;
// the Grants are the authority the credential actually held, which is what makes an outcome
// explicable rather than merely stated.
func testInsertRecordsAnAttributionAndTheGrants(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()
	principal := isolate("insert-attribution")

	attribution, err := authz.ParseAttribution("alice@example.com")
	require.NoError(t, err)

	written := decisionRecord(principal, time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC))
	written.Action = authz.Create
	written.Details.Attribution = attribution
	written.Details.Grants = []string{
		authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()).String(),
		authz.MustNewGrant(authz.RoleSender, authz.AllDomainsAnchor()).String(),
	}

	require.NoError(t, repo.Insert(ctx, written))

	stored, err := read(ctx, principal)
	require.NoError(t, err)
	require.Len(t, stored, 1)

	assert.Equal(t, written.Details, stored[0].Details)
	assert.True(t, written.equal(stored[0]), "wrote %+v, read back %+v", written, stored[0])
}

// A guarded operation reached by a request nothing authenticated is an internal wiring mistake, and
// the register has to be able to hold one: a Record that could not be stored would leave the mistake
// visible only in a log line, which is what this table exists to stop being the only account.
func testInsertRecordsARequestNothingAuthenticated(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()

	// Nothing authenticated the request, so there is no Principal to isolate this Record by and no
	// Grants to record. The empty string is what it stores and the only thing a reader can filter it
	// by, so this reads its own Record back by identifier and says nothing about how many other
	// Records name nobody — a wiring mistake anywhere else in the suite leaves one here too.
	written := decisionRecord("", time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC))
	written.Outcome = authz.NoPrincipal
	written.Details = Details{}

	require.NoError(t, repo.Insert(ctx, written))

	stored, err := read(ctx, "")
	require.NoError(t, err)
	assert.True(t, written.equal(findByID(t, stored, written.ID)),
		"a Record naming nobody did not survive being stored")
}

// The retention an operator configured is what the table holds only if the cleanup takes exactly
// what is expired. Taking more would erase the register a month early; taking less would keep
// personal data past the day it was promised to be gone.
func testDeleteOlderThan(t *testing.T, repo Repository, read Reader) {
	ctx := t.Context()
	principal := isolate("delete-older-than")
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	expired := decisionRecord(principal, cutoff.Add(-time.Second))
	boundary := decisionRecord(principal, cutoff)
	recent := decisionRecord(principal, cutoff.Add(24*time.Hour))
	for _, rec := range []Record{expired, boundary, recent} {
		require.NoError(t, repo.Insert(ctx, rec))
	}

	deleted, err := repo.DeleteOlderThan(ctx, cutoff)
	require.NoError(t, err)
	// The count is what the cleanup logs, and it logs only when it deleted something. Not an
	// equality: the specifications share one Postgres database, so the statement also expires rows
	// this subtest never wrote.
	assert.GreaterOrEqual(t, deleted, int64(1))

	stored, err := read(ctx, principal)
	require.NoError(t, err)
	require.Len(t, stored, 2, "the cleanup took a Record it should have spared")

	// The boundary is exclusive, deliberately: a Record at the instant named survives, so two
	// cleanup runs an hour apart cannot come to two answers about the Record between them.
	assert.True(t, boundary.equal(findByID(t, stored, boundary.ID)),
		"the Record at the cutoff instant survived, but not intact")
	assert.True(t, recent.equal(findByID(t, stored, recent.ID)),
		"the Record after the cutoff survived, but not intact")
}

// decisionRecord is one permitted decision, assembled field by field rather than through NewRecord.
// NewRecord stamps the instant of the call, and every assertion here is about an instant the
// specification chose: what survives a round trip, and what a cutoff a day later spares.
func decisionRecord(principal string, at time.Time) Record {
	return Record{
		ID:         utils.NewID(idPrefix),
		OccurredAt: at,
		Action:     authz.List,
		Outcome:    authz.Allowed,
		Principal:  principal,
		Resource:   authz.Domains().Segments(),
		Details: Details{
			Grants: []string{
				authz.MustNewGrant(
					authz.RoleSender, authz.DomainAnchor(values.MustParse("example.com")),
				).String(),
			},
		},
	}
}

// isolate names a Principal no other subtest names. The specifications share one Postgres database
// and the Reader filters by Principal, so a name of its own is what keeps a Len assertion here from
// counting rows another subtest, or an earlier run against the same database, left behind.
func isolate(subtest string) string {
	return fmt.Sprintf("%s-%d", subtest, time.Now().UnixNano())
}

// findByID picks one Record out of what an implementation returned. By identifier and not by
// position: nothing here orders a read — Repository has no read to order — so indexing into the
// slice would assert an ordering neither implementation promises.
func findByID(t *testing.T, records []Record, id string) Record {
	t.Helper()
	for _, rec := range records {
		if rec.ID == id {
			return rec
		}
	}
	require.FailNowf(t, "record was not stored",
		"no record with id %q among the %d stored", id, len(records))
	return Record{}
}
