package authz_test

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guard is a decorator rather than a bare check because "check, then proceed" has
// to be a single expression there is no falling through: with a bare Require, a
// call site that omits the return compiles, passes review often enough, and
// authorizes everything.
func TestGuardRunsTheOperationWhenAllowed(t *testing.T) {
	ctx := authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example)))

	got, err := authz.Guard(ctx, authz.Update, authz.Domain(example), func() (string, error) {
		return "tracking policy set", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "tracking policy set", got)
}

// A missing Principal is distinguishable from a forbidden one. Both surface as
// permission denied at an edge, but they are very different operational problems:
// one says this credential may not do this, the other says nothing authenticated
// the request at all.
func TestGuardDistinguishesAMissingPrincipalFromAForbiddenOne(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want error
	}{
		{
			name: "nothing authenticated the request",
			ctx:  context.Background(),
			want: authz.ErrNoPrincipal,
		},
		{
			name: "the credential may not do this",
			ctx:  authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(other))),
			want: authz.ErrForbidden,
		},
		{
			name: "the credential holds nothing at all",
			ctx:  authz.NewContext(context.Background(), principalWith()),
			want: authz.ErrForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			_, err := authz.Guard(tc.ctx, authz.Update, authz.Domain(example), func() (struct{}, error) {
				called = true
				return struct{}{}, nil
			})

			assert.ErrorIs(t, err, tc.want)
			assert.False(t, called, "the guarded operation must not run")
		})
	}
}

// A Principal carrying an Attribution it holds no attribute for is refused.
//
// Setting an Attribution performs no check of its own, deliberately: entitlement
// to make the claim depends on the Resource being acted on, which is not known
// there. So it is verified here, where it is — and the consequence is that an
// Attribution can only cause the guarded operation to be refused, never smuggle
// anything past it.
//
// No Role in the seeded catalogue holds attribute, so *every* Attribution is
// refused today. That is ADR 0008's intent and not a gap: with nothing
// authenticated there is no trusted front-end, and an Attribution from an
// unauthenticated caller would let anyone write any name into a record that looks
// authoritative.
func TestGuardRefusesAnAttributionThePrincipalCannotMake(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com")
	ctx := authz.NewContext(context.Background(), owner)

	called := false
	_, err := authz.Guard(ctx, authz.Update, authz.Domain(example), func() (struct{}, error) {
		called = true
		return struct{}{}, nil
	})

	assert.ErrorIs(t, err, authz.ErrForbidden)
	assert.False(t, called, "the operation must not run and be mis-recorded")

	// The same Principal without the claim is allowed, so the refusal is about
	// the Attribution and not about the operation.
	allowed := authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example)))
	_, err = authz.Guard(allowed, authz.Update, authz.Domain(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// The context is transport only — Can never sees it — but the boolean matters:
// "nothing authenticated this request" and "this Principal may do nothing" look
// the same at an edge and are very different when reading a log.
func TestFromContextReportsWhetherAPrincipalTravelled(t *testing.T) {
	_, ok := authz.FromContext(context.Background())
	assert.False(t, ok)

	want := adminOn(authz.RootAnchor())
	got, ok := authz.FromContext(authz.NewContext(context.Background(), want))
	require.True(t, ok)
	assert.Equal(t, want.String(), got.String())
}
