package audit

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnUnreadablePayloadIsAbandonedAndNotRetried is the #396 hot loop asserted at the one seam that
// can reach it: a payload does not become parseable on redelivery, so a Nak here would have the
// server hand the same poisoned message back for as many attempts as it allows, at whatever rate the
// backoff curve ends on. Nothing further down the chain can produce this message without breaking a
// producer, which is why it is pinned here and not in the e2e suite.
func TestAnUnreadablePayloadIsAbandonedAndNotRetried(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()
	logged := captureLogs(t)

	msg := &fakeMsg{data: []byte(`{"id":`)}
	require.NoError(t, h.handleRecordMsg(t.Context(), msg))

	assert.Zero(t, msg.naked, "a permanent fault must not be sent round again")
	assert.Equal(t, 1, msg.termed, "the message must be settled, not left in flight")

	out := logged()
	assert.Contains(t, out, `"level":"ERROR"`, "an abandoned decision must be logged as an error, got %q", out)
}

// TestARecordThatIsNotWellFormedIsAbandoned is the same settlement over the other half of Unmarshal:
// a payload can be valid JSON and still not be worth a row. A Principal absent under an outcome that
// is not no-principal is the case a security reviewer reads this table for — a decision naming nobody
// must not be able to arrive as an ordinary refusal.
func TestARecordThatIsNotWellFormedIsAbandoned(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()

	record := aRecord(t)
	record.Outcome = authz.Denied
	record.Principal = ""

	msg := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), msg))

	assert.Equal(t, 1, msg.termed, "a record that cannot be trusted is abandoned, not stored")
	assert.Zero(t, msg.naked)
	assert.Zero(t, storedRecords(t, record.ID))
}

// TestADecisionIsWrittenDownAndFinishedWith is the ordinary path: one published decision becomes one
// row, and the message is acknowledged so it does not come round again.
func TestADecisionIsWrittenDownAndFinishedWith(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()

	record := aRecord(t)
	msg := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), msg))

	assert.Equal(t, 1, msg.acked, "a written decision is finished with")
	assert.Zero(t, msg.termed)
	assert.Zero(t, msg.naked)
	assert.Equal(t, 1, storedRecords(t, record.ID), "the decision must be in the register")
}

// TestARefusalIsWrittenDownWithItsReason is the other outcome, and the reason a refusal is worth a
// row at all: "denied" alone says that and not why, so the payload has to carry which check refused
// and the authority the Principal did hold.
func TestARefusalIsWrittenDownWithItsReason(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()

	record := aRecord(t)
	record.Outcome = authz.Denied
	record.Details.Reason = "action not permitted on this resource"

	msg := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), msg))
	assert.Equal(t, 1, msg.acked)

	var outcome, reason string
	err := db.QueryRow(t.Context(),
		"SELECT outcome, data->>'reason' FROM audit_records WHERE id = $1", record.ID,
	).Scan(&outcome, &reason)
	require.NoError(t, err)

	assert.Equal(t, string(authz.Denied), outcome)
	assert.Equal(t, record.Details.Reason, reason, "a refusal that does not say why answers nothing")
}

// TestADatabaseFailureSendsTheDecisionBack is the one settlement that comes back. A transient failure
// should cost a delay and not a Record: the register is consulted precisely about the periods when
// things were going wrong, so losing rows to a database that was briefly gone would empty it exactly
// where it matters.
func TestADatabaseFailureSendsTheDecisionBack(t *testing.T) {
	cleanDB(t)

	h := auditHandler{repo: failingRepository{err: errDatabaseGone}, retention: testRetention}
	logged := captureLogs(t)

	record := aRecord(t)
	msg := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), msg))

	assert.Equal(t, 1, msg.naked, "a transient failure must put the decision back on the stream")
	assert.Zero(t, msg.acked, "acknowledging a decision that was not written loses it")
	assert.Zero(t, msg.termed)

	out := logged()
	assert.Contains(t, out, record.ID, "the log must name the record that did not land, got %q", out)
}

// TestARedeliveredDecisionInsertsNothingTheSecondTime is why the identifier comes from the producer.
// A crash between the write and the acknowledgement, or the Nak above, delivers the same payload
// again — and one decision must be in the register once, not twice. Asserted here as well as at the
// repository, because this is the seam a redelivery actually arrives at.
func TestARedeliveredDecisionInsertsNothingTheSecondTime(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()
	record := aRecord(t)

	first := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), first))

	second := decisionMsg(t, record)
	require.NoError(t, h.handleRecordMsg(t.Context(), second))

	assert.Equal(t, 1, first.acked)
	assert.Equal(t, 1, second.acked, "a redelivery is finished with, not left to come round again")
	assert.Equal(t, 1, storedRecords(t, record.ID), "one decision, one row")
}

// TestTwoSimultaneousIdenticalDecisionsStayTwoRows is what a natural key over the columns would have
// collapsed. With one shared token and a front end making two parallel calls this is ordinary rather
// than exotic, and a register that reported one operation where two happened would understate what
// was done.
func TestTwoSimultaneousIdenticalDecisionsStayTwoRows(t *testing.T) {
	cleanDB(t)

	h := newTestHandler()

	// The same decision in every respect an operator could have named, twice — only the producer's
	// identifier tells them apart, which is exactly the distinction a natural key would have lost.
	first := aRecord(t)
	second := first
	second.ID = utils.NewID("audit")

	require.NoError(t, h.handleRecordMsg(t.Context(), decisionMsg(t, first)))
	require.NoError(t, h.handleRecordMsg(t.Context(), decisionMsg(t, second)))

	var count int
	err := db.QueryRow(t.Context(),
		"SELECT COUNT(*) FROM audit_records WHERE principal = $1 AND action = $2",
		first.Principal, string(first.Action),
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "two operations are two rows, however alike they look")
}
