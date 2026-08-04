package authz

import (
	"context"
	"time"
)

// principalKey is the context key under which a Principal travels. Unexported,
// so nothing outside this package can plant one under a colliding key.
type principalKey struct{}

// NewContext carries a Principal from the point that authenticated it to the call site that
// needs it. The context is transport only: Can never sees it, so the decision procedure
// stays a pure function of its arguments.
func NewContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the Principal carried by ctx, if any. The boolean distinguishes
// "nothing authenticated this request" from "this Principal may do nothing", which look the
// same at an edge and are very different when reading a log.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Guard wraps a domain service operation with the authorization it requires, leaving the operation
// unaware of it. A decorator rather than a bare Require call, since a forgotten return after a check
// authorizes everything. An Attribution on the Principal also requires Attribute on the Resource.
//
// Every decision it reaches — permitted, refused, and the request nothing authenticated — is handed
// to the Recorder the context carries, this being the one place that sees Principal, Action,
// Resource and outcome together (ADR 0010). The signature is unchanged by that: the Recorder travels
// in the context and Can stays pure, so a deployment can switch the whole of it off.
func Guard[T any](ctx context.Context, action Action, resource Resource, fn func() (T, error)) (T, error) {
	var zero T

	rec := recorderFrom(ctx)

	p, ok := FromContext(ctx)
	if !ok {
		rec.Record(decision(Principal{}, action, resource, NoPrincipal, "nothing authenticated this request"))
		return zero, ErrNoPrincipal
	}

	if err := Require(p, action, resource); err != nil {
		rec.Record(decision(p, action, resource, Denied, "the Action is not permitted on this Resource"))
		return zero, err
	}

	if p.Attribution() != "" {
		if err := Require(p, Attribute, resource); err != nil {
			rec.Record(decision(p, action, resource, Denied, "the Attribution is not permitted on this Resource"))
			return zero, err
		}
	}

	// Recorded before the operation runs, so the decision is written down even if the operation
	// then fails or the process dies: what is being recorded is what was permitted, which is
	// settled by now, and not what came of it.
	rec.Record(decision(p, action, resource, Allowed, ""))

	return fn()
}

// decision assembles what the Recorder is told, stamping the instant here — the moment the decision
// was reached — rather than leaving it to whatever eventually writes the record down.
func decision(p Principal, action Action, resource Resource, outcome Outcome, reason string) Decision {
	return Decision{
		Principal: p,
		Action:    action,
		Resource:  resource,
		Outcome:   outcome,
		Reason:    reason,
		At:        time.Now().UTC(),
	}
}
