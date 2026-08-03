package authz

import "context"

// principalKey is the context key under which a Principal travels. Unexported,
// so nothing outside this package can plant one under a colliding key.
type principalKey struct{}

// NewContext carries a Principal from the point that authenticated it to the
// call site that needs it.
//
// The context is transport only. Can never sees it, so the decision procedure
// stays a pure function of its arguments.
func NewContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the Principal carried by ctx, if any.
//
// The boolean distinguishes "nothing authenticated this request" from "this
// Principal may do nothing", which look the same at an edge and are very
// different when reading a log.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Guard wraps a domain service operation with the authorization it requires.
//
// The operation itself stays entirely unaware of authorization — it can be
// tested without ever constructing a Principal — and the requirement is
// declared at the point the two are composed.
//
// The reason this is a decorator rather than a Require call inside the operation
// is narrow but real: with a bare check, the call site writes
//
//	if err := Require(...); err != nil { return ... }
//
// and omitting the return is a Go mistake that compiles, passes review often
// enough, and authorizes everything. Here "check, then proceed" is a single
// expression from which there is no falling through.
//
// When the Principal carries an Attribution, Guard also requires Attribute on
// the same Resource. That is where entitlement to make a claim is verified,
// since it depends on what is being acted on: a Principal that sets an
// Attribution it does not hold Attribute for causes the operation to be refused
// rather than performed and mis-recorded.
//
// An operation returning nothing can instantiate T as struct{}.
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
