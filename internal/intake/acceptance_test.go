package intake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

type decisions []authz.Decision

func (d *decisions) Record(v authz.Decision) { *d = append(*d, v) }

func TestMain(m *testing.M) {
	tests.Main(m.Run)
}

func TestAcceptance(t *testing.T) {
	pool, purge, err := tests.TestPostgresInit(schema.Schema)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close(); require.NoError(t, purge()) })
	name := values.MustParse("intake.example.com")
	q := sqlc.New(pool)
	_, err = q.CreateDomain(t.Context(), sqlc.CreateDomainParams{Domain: name.String(), DkimPrivateKey: "private", DkimPublicKey: "public"})
	require.NoError(t, err)
	domain, err := sqlc.NewDomainsRepository(pool).FindByName(t.Context(), name)
	require.NoError(t, err)
	principal := authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name)))
	var audit decisions
	ctx := authz.WithRecorder(authz.NewContext(t.Context(), principal), &audit)
	s := intake.New(sqlc.NewIntakeStore(pool), delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	tplRepo := sqlc.NewTemplatesRepository(pool)
	original, err := templates.NewPersistent(name, "<p>{{ name }}</p>", "Original")
	require.NoError(t, err)
	require.NoError(t, tplRepo.Create(ctx, original))
	request := func() intake.Request {
		return intake.Request{Sender: batch.Sender{Email: "sender@intake.example.com"}, Subject: "Hello", HTML: new("<p>Hello</p>"), Recipients: []intake.Recipient{{Recipient: batch.Recipient{Email: "valid@example.com"}}}}
	}
	// State counts observe atomic acceptance at its persistence seam, including
	// Transient Templates which intentionally cannot be enumerated by Admin.
	counts := func() [3]int {
		t.Helper()
		var got [3]int
		require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM templates), (SELECT count(*) FROM messages), (SELECT count(*) FROM sending_pool_emails)`).Scan(&got[0], &got[1], &got[2]))
		return got
	}
	t.Run("denied intake leaves no new state", func(t *testing.T) {
		before := counts()
		denied := authz.WithRecorder(authz.NewContext(t.Context(), authz.MustNewPrincipal("revoked")), &audit)
		for _, inline := range []bool{true, false} {
			p := request()
			if !inline {
				p.HTML = nil
				p.TemplateID = original.TemplateID()
				p.GlobalFields = map[string]string{"name": "Alice"}
			}
			_, err := s.Accept(denied, domain, p)
			require.ErrorIs(t, err, authz.ErrForbidden)
			require.Equal(t, before, counts())
			require.Equal(t, authz.Denied, audit[len(audit)-1].Outcome)
		}
	})
	t.Run("scheduling failure rolls back all acceptance", func(t *testing.T) {
		// A database trigger fails even after COPY has begun inserting Deliveries.
		_, err := pool.Exec(ctx, `CREATE FUNCTION refuse_intake_delivery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'schedule unavailable'; END $$; CREATE TRIGGER refuse_intake_delivery BEFORE INSERT ON sending_pool_emails FOR EACH ROW EXECUTE FUNCTION refuse_intake_delivery()`)
		require.NoError(t, err)
		defer func() {
			_, err := pool.Exec(context.Background(), `DROP TRIGGER refuse_intake_delivery ON sending_pool_emails; DROP FUNCTION refuse_intake_delivery()`)
			require.NoError(t, err)
		}()
		for _, inline := range []bool{true, false} {
			before := counts()
			p := request()
			if !inline {
				p.HTML = nil
				p.TemplateID = original.TemplateID()
				p.GlobalFields = map[string]string{"name": "Alice"}
			}
			result, err := s.Accept(ctx, domain, p)
			require.ErrorContains(t, err, "schedule unavailable")
			require.Nil(t, result)
			require.Equal(t, before, counts())
			require.Equal(t, authz.Allowed, audit[len(audit)-1].Outcome)
		}
	})
	t.Run("invalid Batch instructions leave no new state", func(t *testing.T) {
		for _, change := range []func(*intake.Request){
			func(p *intake.Request) {
				p.OneClickUnsubscribe = batch.OneClickUnsubscribe{URLTemplate: "http://example.com/leave"}
			},
			func(p *intake.Request) { p.Tracking = tracking.Policy{Opens: tracking.ModeFull} },
			func(p *intake.Request) { p.TrackingError = errors.New("unsupported mode") },
			func(p *intake.Request) { p.Headers = batch.Headers{Cc: []string{"invalid"}} },
		} {
			before := counts()
			p := request()
			change(&p)
			result, err := s.Accept(ctx, domain, p)
			refusal, ok := apperr.As(err)
			require.True(t, ok)
			require.Equal(t, apperr.Invalid, refusal.Kind)
			require.Nil(t, result)
			require.Equal(t, before, counts())
			require.Equal(t, authz.Allowed, audit[len(audit)-1].Outcome)
		}
	})

	t.Run("mixed Recipients preserve decisions and frozen policy", func(t *testing.T) {
		p := request()
		p.Tracking = tracking.Policy{Opens: tracking.ModeAnonymous}
		p.OneClickUnsubscribe = batch.OneClickUnsubscribe{URLTemplate: "https://example.com/leave?token={{ token }}"}
		unreadable := errors.New("unsupported mode")
		p.Recipients = []intake.Recipient{
			{Recipient: batch.Recipient{Email: "valid@example.com", Fields: map[string]string{"token": "a+b"}}},
			{Recipient: batch.Recipient{Email: "  "}, TrackingError: unreadable},
			{Recipient: batch.Recipient{Email: "unreadable@example.com"}, TrackingError: unreadable},
			{Recipient: batch.Recipient{Email: "ceiling@example.com", Tracking: tracking.Policy{Opens: tracking.ModeFull}}},
			{Recipient: batch.Recipient{Email: "unresolved@example.com"}},
		}
		scheduled := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
		p.ScheduledTime = &scheduled
		result, err := s.Accept(ctx, domain, p)
		require.NoError(t, err)
		require.Equal(t, 1, result.AcceptedCount)
		require.Equal(t, []intake.RejectedRecipient{{Email: "  ", Reason: "invalid_email"}, {Email: "unreadable@example.com", Reason: "unsupported_tracking_mode"}, {Email: "ceiling@example.com", Reason: "tracking_above_ceiling"}, {Email: "unresolved@example.com", Reason: "unsubscribe_url_unresolved"}}, result.Rejected)
		// Change the Domain after intake; the Delivery keeps its original Policy.
		_, err = sqlc.NewDomainsRepository(pool).SetTrackingPolicy(ctx, name, tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff})
		require.NoError(t, err)
		d, err := sqlc.NewDeliveryRepository(pool, delivery.DefaultBackoff, delivery.DefaultRetryWindow).Get(ctx, result.Batch.ID(), "valid@example.com")
		require.NoError(t, err)
		require.Equal(t, tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeIdentified}, d.TrackingPolicy())
		require.Equal(t, scheduled, d.ScheduledTime())
		require.Equal(t, scheduled.Add(5*time.Minute), d.NextRetryAt())
		b, err := sqlc.NewBatchRepository(pool).GetByID(ctx, result.Batch.ID())
		require.NoError(t, err)
		require.Equal(t, p.OneClickUnsubscribe, b.OneClickUnsubscribe())
	})
	t.Run("persistent Template is reused or rendered without changing it", func(t *testing.T) {
		for _, replace := range []bool{false, true} {
			p := request()
			p.HTML = nil
			p.TemplateID = original.TemplateID()
			if replace {
				p.GlobalFields = map[string]string{"name": "Alice"}
			}
			result, err := s.Accept(ctx, domain, p)
			require.NoError(t, err)
			tpl, err := tplRepo.GetByID(ctx, result.Batch.TemplateID())
			require.NoError(t, err)
			if replace {
				require.Equal(t, "<p>Alice</p>", tpl.Html())
				require.Equal(t, templates.TypeTransient, tpl.Type())
			} else {
				require.Equal(t, original.TemplateID(), tpl.TemplateID())
			}
			unchanged, err := tplRepo.GetByID(ctx, original.TemplateID())
			require.NoError(t, err)
			require.Equal(t, "<p>{{ name }}</p>", unchanged.Html())
			require.Equal(t, templates.TypePersistent, unchanged.Type())
		}
	})
	t.Run("all refused still accepts a Batch", func(t *testing.T) {
		p := request()
		p.Recipients = []intake.Recipient{{}}
		result, err := s.Accept(ctx, domain, p)
		require.NoError(t, err)
		require.Zero(t, result.AcceptedCount)
		require.Len(t, result.Rejected, 1)
		_, err = sqlc.NewBatchRepository(pool).GetByID(ctx, result.Batch.ID())
		require.NoError(t, err)
	})
}
