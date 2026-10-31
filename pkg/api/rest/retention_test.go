package rest_test

import (
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHistoryExpiresAtThirtyDays(t *testing.T) {
	db, purge, err := credentialDB()
	require.NoError(t, err)
	defer func() {
		if err := purge(); err != nil {
			t.Error(err)
		}
	}()
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor())))
	domain := values.MustParse("example.com")
	ds := domains.NewService(sqlc.NewDomainsRepository(db))
	_, err = ds.CreateDomain(ctx, domain)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc := intake.NewInlineService(sqlc.NewInlineStore(db, func() time.Time { return now }), sqlc.NewDomainsRepository(db), nil, 0)
	svc.Now = func() time.Time { return now }
	req := intake.InlineRequest{Sender: batch.Sender{Email: "from@example.com"}, Subject: "Hello", HTML: "Hello", Recipients: []intake.InlineRecipient{{Email: "to@example.com"}}, Attachments: []intake.Attachment{{Filename: "note.txt", Content: []byte("owned attachment")}}}
	receipt, _, _, err := svc.Create(ctx, domain, "retention", req)
	require.NoError(t, err)
	tpl, err := templates.NewPersistent(domain, "shared", "shared")
	require.NoError(t, err)
	require.NoError(t, sqlc.NewTemplatesRepository(db).Create(ctx, tpl))
	req.HTML = ""
	req.TemplateID = tpl.TemplateID()
	pending, _, _, err := svc.Create(ctx, domain, "pending", req)
	require.NoError(t, err)
	due := now.Add(60 * 24 * time.Hour)
	scheduledReq := req
	scheduledReq.ScheduledAt = &due
	scheduled, _, _, err := svc.Create(ctx, domain, "scheduled", scheduledReq)
	require.NoError(t, err)
	partialReq := req
	partialReq.Recipients = []intake.InlineRecipient{{Email: "to@example.com"}, {Email: "other@example.com"}}
	partial, _, _, err := svc.Create(ctx, domain, "partial", partialReq)
	require.NoError(t, err)
	sr := sqlc.NewStatsRepository(db)
	require.NoError(t, sr.Insert(ctx, stats.NewStat("to@example.com", partial.ID, domain, now, stats.Delivered())))
	ar := sqlc.NewAggregatedStatsRepository(db)
	require.NoError(t, ar.Increment(ctx, domain, now, stats.TypeDelivered))
	stored, err := sqlc.NewBatchRepository(db).GetByID(ctx, batch.ID(receipt.ID))
	require.NoError(t, err)
	createdAt := now
	require.NoError(t, sr.Insert(ctx, stats.NewStat("to@example.com", receipt.ID, domain, now, stats.Delivered())))
	require.NoError(t, sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow).Clean(ctx, batch.ID(receipt.ID), "to@example.com"))
	b, err := svc.ReadBatch(ctx, domain, receipt.ID)
	require.NoError(t, err)
	require.NotNil(t, b.CompletedAt)
	list, err := svc.ListDeliveries(ctx, domain, receipt.ID, "", keyset.Page[string]{Limit: 10})
	require.NoError(t, err)
	h := rest.NewHandlerWithBatches(ds, admintoken.MustParse("secret"), db.Ping, svc, nil, nil)
	read := func(path string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/example.com/batches/"+path, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	now = b.CompletedAt.Add(30*24*time.Hour - time.Microsecond)
	read(receipt.ID, 200)
	require.NoError(t, sqlc.New(db).CleanupBatchHistory(ctx, now))
	_, err = sqlc.NewBatchRepository(db).GetByID(ctx, batch.ID(receipt.ID))
	require.NoError(t, err)
	require.NoError(t, sr.Insert(ctx, stats.NewStat("to@example.com", receipt.ID, domain, now, stats.Bounced(true, 550, "late before expiration"))))
	afterBounce, err := svc.ReadBatch(ctx, domain, receipt.ID)
	require.NoError(t, err)
	require.Equal(t, b.CompletedAt, afterBounce.CompletedAt)
	now = now.Add(time.Microsecond)
	read(receipt.ID, 404)
	read(receipt.ID+"/deliveries", 404)
	read(receipt.ID+"/deliveries/"+list[0].ID, 404)
	read(pending.ID, 200)
	require.NoError(t, sqlc.New(db).CleanupBatchHistory(ctx, now))
	require.NoError(t, sr.Insert(ctx, stats.NewStat("to@example.com", receipt.ID, domain, now, stats.Bounced(true, 550, "late"))))
	read(receipt.ID, 404)
	read(pending.ID, 200)
	read(scheduled.ID, 200)
	read(partial.ID, 200)
	require.NoError(t, sqlc.New(db).CleanupBatchHistory(ctx, now))
	_, err = sqlc.NewBatchRepository(db).GetByID(ctx, batch.ID(receipt.ID))
	require.ErrorIs(t, err, batch.ErrBatchNotFound)
	_, err = sqlc.NewTemplatesRepository(db).GetByID(ctx, stored.TemplateID())
	require.ErrorIs(t, err, templates.ErrTemplateNotFound)
	_, err = sqlc.NewTemplatesRepository(db).GetByID(ctx, tpl.TemplateID())
	require.NoError(t, err)
	events, err := sr.Query(ctx, domain, stats.TimeRange{Start: createdAt.Add(-time.Hour), Stop: now.Add(time.Hour)}, stats.Pagination{Limit: 100})
	require.NoError(t, err)
	require.Len(t, events, 4)
	counts, err := ar.Query(ctx, domain, stats.TimeRange{Start: createdAt.Add(-time.Hour), Stop: now.Add(time.Hour)})
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.Equal(t, int64(1), counts[0].Count)
	originalReq := req
	originalReq.HTML = "Hello"
	originalReq.TemplateID = ""
	expiredReplay, _, ok, err := svc.Create(ctx, domain, "retention", originalReq)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, receipt.ID, expiredReplay.ID)
	originalReq.Subject = "changed"
	_, err = func() (*intake.InlineReceipt, error) {
		r, _, _, e := svc.Create(ctx, domain, "retention", originalReq)
		return r, e
	}()
	require.ErrorIs(t, err, intake.ErrIdempotencyConflict)
	batches, err := svc.ListBatches(ctx, domain, keyset.Page[intake.BatchPosition]{Limit: 100})
	require.NoError(t, err)
	require.Len(t, batches, 3)
	replay, _, replayed, err := svc.Create(ctx, domain, "pending", req)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, pending.ID, replay.ID)
}
