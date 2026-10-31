package rest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/require"
)

func TestDeliveryInitialStateUsesAcceptanceClock(t *testing.T) {
	db, cleanup, err := credentialDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		require.NoError(t, cleanup())
	})
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor())))
	domain := values.MustParse("example.com")
	ds := domains.NewService(sqlc.NewDomainsRepository(db))
	_, err = ds.CreateDomain(ctx, domain)
	require.NoError(t, err)
	// Both sides of database wall time: neither clock skew nor a very short
	// explicit delay may change the intent accepted by the application.
	for _, year := range []int{2000, 2100} {
		for _, scheduled := range []bool{false, true} {
			label := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "/immediate"
			if scheduled {
				label = label[:4] + "/future"
			}
			t.Run(label, func(t *testing.T) {
				now := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
				svc := intake.NewInlineService(sqlc.NewInlineStore(db, func() time.Time { return now }), sqlc.NewDomainsRepository(db), nil, 0, 0)
				svc.Now = func() time.Time { return now }
				req := intake.InlineRequest{Sender: batch.Sender{Email: "from@example.com"}, Subject: "Hello", HTML: "Hello", Recipients: []intake.InlineRecipient{{Email: "to@example.com"}}}
				wantStatus := rest.DeliveryStatus("pending")
				wantTime := now
				if scheduled {
					wantTime = now.Add(time.Microsecond)
					req.ScheduledAt = &wantTime
					wantStatus = "scheduled"
				}
				receipt, _, replay, err := svc.Create(ctx, domain, label, req)
				require.NoError(t, err)
				require.False(t, replay)
				require.True(t, receipt.CreatedAt.Equal(now))
				require.True(t, receipt.ScheduledAt.Equal(wantTime))
				h := rest.NewHandler(rest.Services{Domains: ds, Token: admintoken.MustParse("secret"), Ready: db.Ping, Inline: svc})
				read := func(path string, into any) {
					t.Helper()
					r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/example.com/batches/"+receipt.ID+path, nil)
					r.Header.Set("Authorization", "Bearer secret")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), into))
				}
				var page rest.DeliverySummaryPage
				read("/deliveries?status="+string(wantStatus), &page)
				require.Len(t, page.Items, 1)
				var d rest.Delivery
				read("/deliveries/"+page.Items[0].Id, &d)
				require.Equal(t, wantStatus, d.Status)
				require.True(t, d.ScheduledAt.Equal(wantTime))
				require.True(t, d.UpdatedAt.Equal(receipt.CreatedAt))
				require.Zero(t, d.AttemptCount)
				var b rest.Batch
				read("", &b)
				require.Equal(t, rest.BatchStatus("pending"), b.Status)
				require.Nil(t, b.CompletedAt)
				require.Nil(t, b.ExpiresAt)
				if scheduled {
					require.EqualValues(t, 1, b.DeliveryCounts.Scheduled)
				} else {
					require.EqualValues(t, 1, b.DeliveryCounts.Pending)
				}
				// Replaying after the requested send time must preserve acceptance.
				now = now.Add(time.Hour)
				again, _, replay, err := svc.Create(ctx, domain, label, req)
				require.NoError(t, err)
				require.True(t, replay)
				require.Equal(t, receipt.ID, again.ID)
				read("/deliveries", &page)
				require.Len(t, page.Items, 1)
				require.Equal(t, wantStatus, page.Items[0].Status)
			})
		}
	}
}
