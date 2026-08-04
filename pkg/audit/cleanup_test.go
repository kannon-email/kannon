package audit

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/audit"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertAt puts one Record in the register as though it had been decided at the given instant, which
// is what the sweep reads — the producer stamps the instant, so backdating a fixture is the honest way
// to make one expired.
func insertAt(t *testing.T, at time.Time) audit.Record {
	t.Helper()

	record := aRecord(t)
	record.ID = utils.NewID("audit")
	record.OccurredAt = at

	require.NoError(t, sq.NewAuditRepository(db).Insert(t.Context(), record))
	return record
}

// TestTheSweepDeletesWhatHasExpiredAndKeepsWhatHasNot is the retention enforced: the operator names a
// window, and what falls outside it stops existing. The obligation this discharges is legal rather
// than operational — an Audit Record holds an Attribution, which is personal data — so the boundary
// is worth an assertion in both directions rather than only the deleting one.
func TestTheSweepDeletesWhatHasExpiredAndKeepsWhatHasNot(t *testing.T) {
	cleanDB(t)

	expired := insertAt(t, time.Now().Add(-2*testRetention))
	recent := insertAt(t, time.Now().Add(-time.Hour))

	h := newTestHandler()
	require.NoError(t, runner.Run(t.Context(), h.cleanupCycle, runner.MaxLoop(1)))

	assert.Zero(t, storedRecords(t, expired.ID), "a Record past its retention must not be kept")
	assert.Equal(t, 1, storedRecords(t, recent.ID), "a Record inside the window must survive the sweep")
}

// TestTheSweepStaysSilentWhenNothingExpired keeps the log line a fact rather than a heartbeat. It runs
// hourly on every deployment that has the feature on, and an operator who reads "audit cleanup" in the
// logs has to be able to conclude that Records were in fact deleted.
func TestTheSweepStaysSilentWhenNothingExpired(t *testing.T) {
	cleanDB(t)

	insertAt(t, time.Now().Add(-time.Hour))

	logged := captureLogs(t)

	h := newTestHandler()
	require.NoError(t, runner.Run(t.Context(), h.cleanupCycle, runner.MaxLoop(1)))

	assert.NotContains(t, logged(), "audit cleanup",
		"a sweep that deleted nothing has nothing to report")
}

// TestTheSweepIsIdempotent is why replicas of this worker are harmless: whichever gets there first
// does the work, and a second run over the same window finds nothing and says nothing.
func TestTheSweepIsIdempotent(t *testing.T) {
	cleanDB(t)

	expired := insertAt(t, time.Now().Add(-2*testRetention))

	h := newTestHandler()
	require.NoError(t, runner.Run(t.Context(), h.cleanupCycle, runner.MaxLoop(2)))

	assert.Zero(t, storedRecords(t, expired.ID))
}

// TestASweepThatFailsDoesNotTakeTheProcessDown is user story 18 of #443: an audit problem must never
// be an outage. The sweep runs in an errgroup this worker shares with the API whenever an operator
// co-locates them — standalone and the Kubernetes manifest both do — so an error returned out of the
// loop would answer a database that was briefly gone by killing a healthy API. It logs and carries on.
func TestASweepThatFailsDoesNotTakeTheProcessDown(t *testing.T) {
	logged := captureLogs(t)

	h := auditHandler{repo: failingRepository{err: errDatabaseGone}, retention: testRetention}
	require.NoError(t, runner.Run(t.Context(), h.cleanupCycle, runner.MaxLoop(2)),
		"a failed sweep must not reach the errgroup")

	assert.Contains(t, logged(), "cannot delete expired Audit Records",
		"a sweep that failed silently would leave the retention unenforced with nothing to show it")
}
