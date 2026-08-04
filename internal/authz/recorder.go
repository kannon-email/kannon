package authz

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Outcome is what an authorization decision came to. Three values and not two: a request that
// reached a guarded operation with nothing authenticating it is an internal wiring mistake rather
// than an ordinary refusal, and a register that spelled the two the same could not say so.
type Outcome string

const (
	// Allowed is a Principal that may, on a Resource it reaches.
	Allowed Outcome = "allowed"

	// Denied is a resolved Principal that may not — either the Action or, when a claim was
	// made, the Attribute the claim requires.
	Denied Outcome = "denied"

	// NoPrincipal is a guarded operation reached by a request nothing authenticated. Never a
	// caller's doing: an edge either authenticates a request or refuses it, so this says a
	// path into the system was wired without one.
	NoPrincipal Outcome = "no-principal"
)

// ParseOutcome validates a string against the closed vocabulary, for the boundary at which a
// stored or wire value becomes a typed one.
func ParseOutcome(s string) (Outcome, error) {
	switch Outcome(s) {
	case Allowed, Denied, NoPrincipal:
		return Outcome(s), nil
	default:
		return "", fmt.Errorf("unknown outcome %q", s)
	}
}

// Decision is one authorization decision as Guard reached it: who asked, what for, on what, and
// what came of it. The whole of what an Audit Record is made from (ADR 0010), assembled here
// because Guard is the only place that sees all four together.
type Decision struct {
	// Principal is the credential the request authenticated as. The zero Principal when
	// Outcome is NoPrincipal, which is the only case in which nothing authenticated it.
	Principal Principal

	// Action and Resource are what was asked for, exactly as the guarded call site named them.
	Action   Action
	Resource Resource

	Outcome Outcome

	// Reason names which check refused, empty when none did. Not a restatement of Outcome:
	// Guard refuses for two different reasons — the Action, or the Attribute a claim needs —
	// and "denied" alone cannot tell an operator which of the two happened.
	Reason string

	// At is the instant the decision was reached, stamped here rather than wherever the record
	// eventually lands: a consumer catching up after a stop would otherwise date every record
	// it writes to the moment it recovered.
	At time.Time
}

// Recorder is told of every authorization decision Guard reaches. An interface so that where
// records go is a wiring decision and not a fact about this package: a deployment that wants an
// audit trail installs one that publishes, and one that wants none installs nothing and keeps the
// logging default (ADR 0010).
//
// Record returns no error and takes no context, both deliberately. A record must never fail the
// operation it describes, so there is no error for a caller to do anything with; and a decision
// happened whether or not the request that caused it was subsequently cancelled, so the request's
// context has no business governing whether it is written down.
type Recorder interface {
	Record(d Decision)
}

// recorderKey is the context key under which a Recorder travels. Unexported, like the Principal's,
// so nothing outside this package can plant one under a colliding key.
type recorderKey struct{}

// WithRecorder installs the Recorder that every Guard call beneath ctx reports to. Carried in the
// context rather than passed to Guard or held by each service: a Recorder is a property of the
// process — one middleware installs it for every request — and threading it through six domain
// constructors would propagate a dependency none of them uses, for a feature that may be off.
func WithRecorder(ctx context.Context, r Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// recorderFrom returns the Recorder installed on ctx, or the logging one when nothing installed
// any. There is no "no recorder" case on purpose: a forgotten middleware then costs the audit
// table and not the record, which is the failure mode ADR 0009 already considered sufficient.
func recorderFrom(ctx context.Context) Recorder {
	if r, ok := ctx.Value(recorderKey{}).(Recorder); ok && r != nil {
		return r
	}
	return LogRecorder()
}

// LogRecorder is what a deployment that configured nothing gets, and what any other Recorder
// decorates rather than replaces: enabling an audit table must not take away the lines an operator
// already relies on. Exported for that decoration, and for a test to state the default explicitly.
func LogRecorder() Recorder {
	return logRecorder{}
}

// logRecorder writes exactly what Guard wrote before there was a Recorder at all: a debug line for
// every check, and an info line for an attributed operation that was permitted. The levels are
// load-bearing and not a preference — ADR 0009 put the attributed record at info because it holds
// personal data an operator has to be able to find, and left every other check at debug so that
// the two could be told apart. Generalising this to every operation at info would undo that.
type logRecorder struct{}

func (logRecorder) Record(d Decision) {
	// Nothing authenticated the request, so there is no check to report and no Principal to
	// name. The edge that let it through logs the refusal (internal/authzconnect), and always
	// did; a line here would be a second account of one event.
	if d.Outcome == NoPrincipal {
		return
	}

	slog.Debug("RBAC check", "principal", d.Principal, "action", d.Action, "resource", d.Resource)

	// Written for a permitted operation only, as it always has been: a refused claim never
	// became an act performed in anybody's name, so recording one would name a person for
	// something that did not happen.
	if d.Outcome == Allowed && d.Principal.Attribution() != "" {
		slog.Info("attributed operation",
			"principal", d.Principal.ID(), "attribution", d.Principal.Attribution(),
			"action", d.Action, "resource", d.Resource)
	}
}
