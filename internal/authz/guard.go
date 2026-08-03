package authz

import "context"

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
func Guard[T any](ctx context.Context, action Action, resource Resource, fn func() (T, error)) (T, error) {
	var zero T

	p, ok := FromContext(ctx)
	if !ok {
		return zero, ErrNoPrincipal
	}

	if err := Require(p, action, resource); err != nil {
		return zero, err
	}

	if p.Attribution() != "" {
		if err := Require(p, Attribute, resource); err != nil {
			return zero, err
		}
	}

	return fn()
}
