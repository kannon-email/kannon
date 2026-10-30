package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

// unavailableStore represents an unavailable database: denied intake must still
// report the authority decision, without touching storage.
type unavailableStore struct{}

func (unavailableStore) FindByDomain(context.Context, values.DomainName, string) (*templates.Template, error) {
	panic("unauthorized storage access")
}
func (unavailableStore) Accept(context.Context, *templates.Template, *batch.Batch, []*delivery.Delivery) error {
	panic("unauthorized persistence")
}

func TestDeniedIntakeDoesNotReachStorage(t *testing.T) {
	name, err := values.Parse("example.com")
	require.NoError(t, err)
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("revoked"))
	s := intake.New(unavailableStore{}, nil, 0)
	for _, p := range []intake.Request{
		{HTML: new("<p>Hello</p>")},
		{TemplateID: "persistent@example.com", GlobalFields: map[string]string{"name": "Alice"}},
	} {
		p.Sender = batch.Sender{Email: "sender@example.com"}
		p.Subject = "Hello"
		_, err := s.Accept(ctx, domain, p)
		require.ErrorIs(t, err, authz.ErrForbidden)
	}
}

// capturedStore is a database adapter that retains accepted domain records so
// the intake seam can observe the Retry Budget before database rehydration.
type capturedStore struct{ deliveries []*delivery.Delivery }

func (*capturedStore) FindByDomain(context.Context, values.DomainName, string) (*templates.Template, error) {
	panic("inline HTML needs no lookup")
}
func (s *capturedStore) Accept(_ context.Context, _ *templates.Template, _ *batch.Batch, ds []*delivery.Delivery) error {
	s.deliveries = ds
	return nil
}

func TestIntakeWiresRetryBudget(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	scheduled := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		backoff  delivery.BackoffPolicy
		window   time.Duration
		delay    time.Duration
		canRetry bool
	}{
		{name: "defaults", delay: 5 * time.Minute, canRetry: true},
		{name: "configured budget exhausted", backoff: delivery.ExponentialBackoff{Base: 2 * time.Minute}, window: time.Minute, delay: 2 * time.Minute},
		{name: "configured budget permits retry", backoff: delivery.ExponentialBackoff{Base: 2 * time.Minute}, window: 3 * time.Minute, delay: 2 * time.Minute, canRetry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &capturedStore{}
			s := intake.New(store, tc.backoff, tc.window)
			_, err := s.Accept(ctx, domain, intake.Request{HTML: new("Hello"), Subject: "Hello", Sender: batch.Sender{Email: "sender@example.com"}, ScheduledTime: &scheduled, Recipients: []intake.Recipient{{Recipient: batch.Recipient{Email: "recipient@example.com"}}}})
			require.NoError(t, err)
			require.Len(t, store.deliveries, 1)
			require.Equal(t, scheduled.Add(tc.delay), store.deliveries[0].NextRetryAt())
			require.Equal(t, tc.canRetry, store.deliveries[0].CanRetry())
		})
	}
}

func TestIntakeRequiresAuthorityOnPersistenceDomain(t *testing.T) {
	source := values.MustParse("a.example")
	target := values.MustParse("b.example")
	domain := domains.Load(domains.LoadParams{Domain: target})
	principal := authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(source)))
	var audit decisions
	ctx := authz.WithRecorder(authz.NewContext(t.Context(), principal), &audit)
	s := intake.New(unavailableStore{}, nil, 0)
	for _, p := range []intake.Request{{HTML: new("Hello")}, {TemplateID: "existing"}} {
		p.Sender = batch.Sender{Email: "sender@a.example"}
		p.Subject = "Hello"
		_, err := s.Accept(ctx, domain, p)
		require.ErrorIs(t, err, authz.ErrForbidden)
		require.Equal(t, authz.Batches(target), audit[len(audit)-1].Resource)
		require.Equal(t, authz.Denied, audit[len(audit)-1].Outcome)
	}
}

func TestIntakeRejectsUnrelatedSenderDespiteAuthority(t *testing.T) {
	name := values.MustParse("b.example")
	domain := domains.Load(domains.LoadParams{Domain: name})
	principal := authz.MustNewPrincipal("key",
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name)),
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(values.MustParse("a.example"))))
	var audit decisions
	ctx := authz.WithRecorder(authz.NewContext(t.Context(), principal), &audit)
	s := intake.New(unavailableStore{}, nil, 0)
	_, err := s.Accept(ctx, domain, intake.Request{HTML: new("Hello"), Subject: "Hello", Sender: batch.Sender{Email: "sender@a.example"}})
	require.ErrorIs(t, err, authz.ErrForbidden)
	require.Len(t, audit, 1)
	require.Equal(t, authz.Denied, audit[0].Outcome)
	require.Equal(t, authz.Batches(name), audit[0].Resource)
	require.NotEmpty(t, audit[0].Reason)
}
