package authz_test

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Recorder a deployment gets when it configured nothing writes exactly what Guard wrote before
// there was a Recorder at all, at the levels it wrote them at. Asserted rather than assumed, because
// the levels are the whole of ADR 0009's record: an attributed operation is at info because it names
// a person an operator has to be able to find, and every other check is at debug so that the two can
// be told apart. Generalising this into every operation at info would silently undo that, and this is
// the test that would fail.
func TestTheDefaultRecorderWritesTodaysLinesAtTodaysLevels(t *testing.T) {
	logged := captureSlog(t)

	ctx := authz.NewContext(context.Background(),
		adminOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com"))
	_, err := authz.Guard(ctx, authz.Create, authz.APIKeys(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)

	record := logged.String()
	assert.Contains(t, record, `level=DEBUG msg="RBAC check"`)
	assert.Contains(t, record, `level=INFO msg="attributed operation"`)
}

// An operation nobody was named for writes the debug line and nothing at info. The pair with the test
// above: were the record generalised to every operation, an operator's info log would fill with
// operations naming no person, and the ones that do would be unfindable among them.
func TestTheDefaultRecorderWritesNothingAtInfoWithoutAnAttribution(t *testing.T) {
	logged := captureSlog(t)

	ctx := authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example)))
	_, err := authz.Guard(ctx, authz.Create, authz.APIKeys(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)

	record := logged.String()
	assert.Contains(t, record, `level=DEBUG msg="RBAC check"`)
	assert.NotContains(t, record, "level=INFO")
}

// recordingRecorder collects what it is told, for the one assertion no higher seam can make: the e2e
// suite reaches a permitted and a refused decision through real calls, but it cannot reach a request
// that arrived at a guarded operation with nothing authenticating it — an edge either authenticates a
// request or refuses it, so producing one would mean breaking the wiring mid-chain.
type recordingRecorder struct {
	decisions []authz.Decision
}

func (r *recordingRecorder) Record(d authz.Decision) {
	r.decisions = append(r.decisions, d)
}

// Every decision is recorded, and the three are told apart. A request nothing authenticated is
// recorded distinctly from an ordinary refusal, because it is an internal wiring mistake rather than
// a caller's doing, and a register that spelled the two the same could not say so (ADR 0010).
func TestGuardRecordsEveryDecisionAndTellsTheThreeApart(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		wantOutcome authz.Outcome
		wantWhom    string
		wantErr     error
	}{
		{
			name:        "the credential may",
			ctx:         authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example))),
			wantOutcome: authz.Allowed,
			wantWhom:    "key-1@example.com",
		},
		{
			name:        "the credential may not",
			ctx:         authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(other))),
			wantOutcome: authz.Denied,
			wantWhom:    "key-1@example.com",
			wantErr:     authz.ErrForbidden,
		},
		{
			name:        "nothing authenticated the request",
			ctx:         context.Background(),
			wantOutcome: authz.NoPrincipal,
			wantWhom:    "",
			wantErr:     authz.ErrNoPrincipal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingRecorder{}
			_, err := authz.Guard(authz.WithRecorder(tc.ctx, rec), authz.Update, authz.Domain(example),
				func() (struct{}, error) { return struct{}{}, nil })

			// The caller still gets what it always got: what a Recorder is told is a record of
			// the decision and never a substitute for it.
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}

			require.Len(t, rec.decisions, 1)
			got := rec.decisions[0]
			assert.Equal(t, tc.wantOutcome, got.Outcome)
			assert.Equal(t, tc.wantWhom, got.Principal.ID())
			assert.Equal(t, authz.Update, got.Action)
			assert.Equal(t, authz.Domain(example).Segments(), got.Resource.Segments())
			assert.False(t, got.At.IsZero(), "a decision without an instant cannot be dated by a worker")

			// A refusal says which check refused. "Denied" alone tells a reviewer that and not
			// why, which is the difference between a register and an alarm.
			if tc.wantOutcome == authz.Allowed {
				assert.Empty(t, got.Reason)
			} else {
				assert.NotEmpty(t, got.Reason)
			}
		})
	}
}

// A claim the Principal may not make is refused, and the refusal is recorded as one — with a reason
// naming the Attribution rather than the Action, since the Action was permitted and only the claim
// was not. Without that, an operator reading the table would see a send refused and go looking for a
// missing Grant on Batches that is in fact present.
func TestGuardRecordsARefusedAttributionAsItsOwnRefusal(t *testing.T) {
	rec := &recordingRecorder{}
	claiming := senderOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com")
	ctx := authz.WithRecorder(authz.NewContext(context.Background(), claiming), rec)

	_, err := authz.Guard(ctx, authz.Create, authz.Batches(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.ErrorIs(t, err, authz.ErrForbidden)

	require.Len(t, rec.decisions, 1)
	assert.Equal(t, authz.Denied, rec.decisions[0].Outcome)
	assert.Contains(t, rec.decisions[0].Reason, "Attribution")
}

// A Recorder installed on the context replaces the default one and does not silence it by accident:
// the two compose by decoration, which is the property that lets an operator turn the table on
// without losing the log lines they already rely on. Here the installed Recorder is not a decorator,
// so nothing is logged — which is what proves the default is not written unconditionally.
func TestAnInstalledRecorderReplacesTheDefaultRatherThanRunningBesideIt(t *testing.T) {
	logged := captureSlog(t)

	rec := &recordingRecorder{}
	ctx := authz.WithRecorder(
		authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example))), rec)
	_, err := authz.Guard(ctx, authz.Update, authz.Domain(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)

	assert.Len(t, rec.decisions, 1)
	assert.NotContains(t, logged.String(), "RBAC check")
}

// ParseOutcome is the boundary at which a stored or wire value becomes a typed one. It refuses what
// it does not know, so a payload carrying a fourth outcome is a fault in the message and is abandoned
// there, rather than reaching the table as a value nothing can interpret.
func TestParseOutcomeRefusesWhatIsNotInTheVocabulary(t *testing.T) {
	for _, want := range []authz.Outcome{authz.Allowed, authz.Denied, authz.NoPrincipal} {
		got, err := authz.ParseOutcome(string(want))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	_, err := authz.ParseOutcome("permitted")
	assert.Error(t, err)
}
