package authz

import (
	"context"
	"log/slog"
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
// authorizes everything. An Attribution on the Principal also requires Attribute on the Resource,
// and is recorded here once entitled — this being the one place that sees claim, Action and Resource.
func Guard[T any](ctx context.Context, action Action, resource Resource, fn func() (T, error)) (T, error) {
	var zero T

	p, ok := FromContext(ctx)
	if !ok {
		return zero, ErrNoPrincipal
	}

	slog.Debug("RBAC check", "principal", p, "action", action, "resource", resource)

	if err := Require(p, action, resource); err != nil {
		return zero, err
	}

	if p.Attribution() != "" {
		if err := Require(p, Attribute, resource); err != nil {
			return zero, err
		}
		record(p, action, resource)
	}

	return fn()
}

// record writes down an operation a Principal asked for in someone's name. A log line and not a
// row: where an Attribution is persisted is still open, and the choice needs a retention policy
// for personal data (ADR 0009). Written once the claim is entitled and before the operation runs,
// so the claim is recorded even if the operation then fails or the process dies — what is being
// recorded is who asked, which is settled by then, and not what came of it.
//
// The authenticated Principal's identifier is logged beside the claim, never instead of it: one
// was checked and the other cannot be, and a record that blurred them would read as though
// Kannon knew the person it names.
func record(p Principal, action Action, resource Resource) {
	slog.Info("attributed operation",
		"principal", p.ID(), "attribution", p.Attribution(), "action", action, "resource", resource)
}
