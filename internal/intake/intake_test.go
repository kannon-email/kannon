package intake_test

import (
	"context"
	"fmt"
	"strings"
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
	s := intake.New(unavailableStore{}, nil, 0, 0)
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
			s := intake.New(store, tc.backoff, tc.window, 0)
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
	s := intake.New(unavailableStore{}, nil, 0, 0)
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
	s := intake.New(unavailableStore{}, nil, 0, 0)
	_, err := s.Accept(ctx, domain, intake.Request{HTML: new("Hello"), Subject: "Hello", Sender: batch.Sender{Email: "sender@a.example"}})
	require.ErrorIs(t, err, authz.ErrForbidden)
	require.Len(t, audit, 1)
	require.Equal(t, authz.Denied, audit[0].Outcome)
	require.Equal(t, authz.Batches(name), audit[0].Resource)
	require.NotEmpty(t, audit[0].Reason)
}

func TestSenderRefusalDescribesApplicationDecision(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	var audit decisions
	ctx = authz.WithRecorder(ctx, &audit)
	_, err := intake.New(unavailableStore{}, nil, 0, 0).Accept(ctx, domain, intake.Request{
		Sender: batch.Sender{Email: "sender@other.example"}, HTML: new("Hello"),
	})
	var refusal *intake.SenderNotAuthorized
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "other.example", refusal.Host)
	require.Equal(t, "example.com", refusal.Domain.String())
	require.ErrorIs(t, err, authz.ErrForbidden)
	require.Len(t, audit, 1)
	require.Equal(t, authz.Denied, audit[0].Outcome)
}

// The Envelope travels as one NATS message, so intake refuses a Batch over the configured
// payload rather than accepting work the Dispatcher can never hand on. The payload is the subject
// and the HTML plus each attachment as base64.
func TestIntakeRefusesMessageTooLarge(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	request := func(attachment int) intake.Request {
		return intake.Request{
			HTML: new("Hello!!"), Subject: "Hello", Sender: batch.Sender{Email: "sender@example.com"},
			Recipients:  []intake.Recipient{{Recipient: batch.Recipient{Email: "recipient@example.com"}}},
			Attachments: batch.Attachments{"a.pdf": make([]byte, attachment)},
		}
	}

	// 516 raw bytes encode to 688; with the 5-byte subject and the 7-byte body that is 700,
	// exactly the limit.
	store := &capturedStore{}
	_, err := intake.New(store, nil, 0, 700).Accept(ctx, domain, request(516))
	require.NoError(t, err)
	require.Len(t, store.deliveries, 1)

	// 519 raw bytes encode to 692; with the text that is 704, past the limit.
	_, err = intake.New(unavailableStore{}, nil, 0, 700).Accept(ctx, domain, request(519))
	require.ErrorIs(t, err, intake.ErrMessageTooLarge)
}

// The legacy intake holds the same bound on Recipients as REST: each one is measured against the
// payload limit before it is accepted, so the bound is what keeps one request's work in proportion.
func TestIntakeRefusesMoreThanTwoThousandRecipients(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	request := func(n int) intake.Request {
		recipients := make([]intake.Recipient, n)
		for i := range recipients {
			recipients[i] = intake.Recipient{Recipient: batch.Recipient{Email: fmt.Sprintf("r%d@example.com", i)}}
		}
		return intake.Request{HTML: new("<p>Hi</p>"), Subject: "Hello", Sender: batch.Sender{Email: "sender@example.com"}, Recipients: recipients}
	}

	store := &capturedStore{}
	_, err := intake.New(store, nil, 0, 500<<10).Accept(ctx, domain, request(2000))
	require.NoError(t, err)
	require.Len(t, store.deliveries, 2000)

	_, err = intake.New(unavailableStore{}, nil, 0, 500<<10).Accept(ctx, domain, request(2001))
	require.ErrorIs(t, err, intake.ErrTooManyRecipients)
}

// Global fields grow the content before it can be measured, so they are substituted under the
// payload limit: a body of well under a megabyte that would render to gigabytes is refused, and
// never built.
func TestIntakeRefusesGlobalFieldsPastThePayload(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	_, err := intake.New(unavailableStore{}, nil, 0, 500<<10).Accept(ctx, domain, intake.Request{
		HTML: new(strings.Repeat("{{a}}", 100_000)), Subject: "Hello", Sender: batch.Sender{Email: "sender@example.com"},
		GlobalFields: map[string]string{"a": strings.Repeat("x", 50_000)},
		Recipients:   []intake.Recipient{{Recipient: batch.Recipient{Email: "recipient@example.com"}}},
	})
	require.ErrorIs(t, err, intake.ErrMessageTooLarge)
}

// A Recipient's own fields are only substituted by the Dispatcher, so intake measures each
// Recipient's message as the Builder will render it, and Rejects the one that cannot fit.
func TestIntakeRejectsARecipientWhoseFieldsPassThePayload(t *testing.T) {
	name := values.MustParse("example.com")
	domain := domains.Load(domains.LoadParams{Domain: name})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	big := map[string]string{"a": strings.Repeat("x", 50_000)}
	store := &capturedStore{}
	result, err := intake.New(store, nil, 0, 500<<10).Accept(ctx, domain, intake.Request{
		HTML: new(strings.Repeat("{{a}}", 100_000)), Subject: "Hello", Sender: batch.Sender{Email: "sender@example.com"},
		OneClickUnsubscribe: batch.OneClickUnsubscribe{URLTemplate: "https://example.com/leave?t={{ t }}" + strings.Repeat("{{t}}", 10_000)},
		Recipients: []intake.Recipient{
			{Recipient: batch.Recipient{Email: "body@example.com", Fields: map[string]string{"a": big["a"], "t": "1"}}},
			{Recipient: batch.Recipient{Email: "fits@example.com", Fields: map[string]string{"a": "", "t": "1"}}},
			{Recipient: batch.Recipient{Email: "unsubscribe@example.com", Fields: map[string]string{"a": "", "t": big["a"]}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []intake.RejectedRecipient{
		{Email: "body@example.com", Reason: intake.ReasonMessageTooLarge},
		{Email: "unsubscribe@example.com", Reason: intake.ReasonMessageTooLarge},
	}, result.Rejected)
	require.Len(t, store.deliveries, 1)
	require.Equal(t, "fits@example.com", store.deliveries[0].Email())
}
