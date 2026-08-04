package authz_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guard is a decorator rather than a bare check because "check, then proceed" has to be one
// expression with no falling through: with a bare Require, a call site that omits the return
// compiles, passes review often enough, and authorizes everything.
func TestGuardRunsTheOperationWhenAllowed(t *testing.T) {
	ctx := authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example)))

	got, err := authz.Guard(ctx, authz.Update, authz.Domain(example), func() (string, error) {
		return "tracking policy set", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "tracking policy set", got)
}

// A missing Principal is distinguishable from a forbidden one. Both surface as permission denied
// at an edge, but they are different operational problems: one says this credential may not do
// this, the other that nothing authenticated the request at all.
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

// A Principal carrying an Attribution it holds no attribute for is refused. Setting one checks
// nothing, since entitlement depends on the Resource, so it is verified here — an Attribution can
// only cause a refusal. A send key is the case that matters: it may create a Batch, and must not
// be able to say somebody else asked for it.
func TestGuardRefusesAnAttributionThePrincipalCannotMake(t *testing.T) {
	claiming := senderOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com")
	ctx := authz.NewContext(context.Background(), claiming)

	called := false
	_, err := authz.Guard(ctx, authz.Create, authz.Batches(example), func() (struct{}, error) {
		called = true
		return struct{}{}, nil
	})

	assert.ErrorIs(t, err, authz.ErrForbidden)
	assert.False(t, called, "the operation must not run and be mis-recorded")

	// The same Principal without the claim is allowed, so the refusal is about
	// the Attribution and not about the operation.
	allowed := authz.NewContext(context.Background(), senderOn(authz.DomainAnchor(example)))
	_, err = authz.Guard(allowed, authz.Create, authz.Batches(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// The Principal that may: admin holds attribute, so the operation runs with the claim attached to
// the Principal it carries. The operation can therefore read who asked — what it must never do is
// decide anything from it, which is the property the whole design rests on (ADR 0008).
func TestGuardRunsAnAttributedOperationThePrincipalMayMake(t *testing.T) {
	ctx := authz.NewContext(context.Background(),
		adminOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com"))

	got, err := authz.Guard(ctx, authz.Update, authz.Domain(example), func() (authz.Attribution, error) {
		p, _ := authz.FromContext(ctx)
		return p.Attribution(), nil
	})

	require.NoError(t, err)
	assert.Equal(t, authz.Attribution("alice@corp.com"), got)
}

// Recording the claim is the whole point of permitting it, so the record is asserted rather than
// assumed. It names the authenticated credential beside the claim — one was checked and the other
// cannot be — and the operation it was made for, since a name with no act attached records nothing.
func TestGuardRecordsAnAttributedOperation(t *testing.T) {
	logged := captureSlog(t)

	ctx := authz.NewContext(context.Background(),
		adminOn(authz.DomainAnchor(example)).WithAttribution("alice@corp.com"))
	_, err := authz.Guard(ctx, authz.Create, authz.APIKeys(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)

	record := logged.String()
	assert.Contains(t, record, "attributed operation")
	assert.Contains(t, record, "principal=key-1@example.com")
	assert.Contains(t, record, "attribution=alice@corp.com")
	assert.Contains(t, record, "action=create")
	assert.Contains(t, record, "resource=domains/example.com/apikeys")
}

// An operation nobody was named for is recorded by nothing at this level: the RBAC line every check
// writes is at debug, and this record is at info because it holds personal data an operator has to
// be able to find. Were it written for every operation, the two would be indistinguishable.
func TestGuardRecordsNothingWithoutAnAttribution(t *testing.T) {
	logged := captureSlog(t)

	ctx := authz.NewContext(context.Background(), adminOn(authz.DomainAnchor(example)))
	_, err := authz.Guard(ctx, authz.Create, authz.APIKeys(example), func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)

	assert.NotContains(t, logged.String(), "attributed operation")
}

// captureSlog redirects the default logger for the duration of one test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
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
