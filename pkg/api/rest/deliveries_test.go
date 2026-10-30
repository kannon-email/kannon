package rest_test

import (
	"encoding/json"
	"errors"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeliverySurvivesPoolAndLateBounce(t *testing.T) {
	db, cleanup, err := credentialDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	ds := domains.NewService(sqlc.NewDomainsRepository(db))
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor())))
	name := values.MustParse("example.com")
	if _, err := ds.CreateDomain(ctx, name); err != nil {
		t.Fatal(err)
	}
	svc := intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0)
	receipt, _, _, err := svc.Create(ctx, name, "history", intake.InlineRequest{Sender: batch.Sender{Email: "from@example.com"}, Subject: "Hello", HTML: "Hello", Recipients: []intake.InlineRecipient{{Email: "to@example.com"}, {Email: "refused"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Discover the accepted fixture's opaque ID; all observations go through HTTP.
	var id string
	if err := db.QueryRow(ctx, `SELECT id FROM delivery_intake_facts WHERE batch_id=$1`, receipt.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	h := rest.NewHandlerWithBatches(ds, admintoken.MustParse("secret"), db.Ping, svc, nil, nil)
	read := func(domain, bid string, status int) rest.Delivery {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/"+domain+"/batches/"+bid+"/deliveries/"+id, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var d rest.Delivery
		if status == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	if d := read("example.com", receipt.ID, 200); d.Status != "pending" || d.Email != "to@example.com" {
		t.Fatal(d)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/example.com/batches/"+receipt.ID, nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("batch read: %d %s", w.Code, w.Body)
	}
	var b rest.Batch
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Status != "pending" || b.DeliveryCounts.Pending != 1 || b.CompletedAt != nil || b.Content.Html != "Hello" {
		t.Fatal(b)
	}

	get := func(path string, code int) []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return rec.Body.Bytes()
	}
	base := "/v1beta1/domains/example.com/batches"
	var page rest.BatchSummaryPage
	if err := json.Unmarshal(get(base+"?limit=1", 200), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Id != receipt.ID || page.NextCursor != nil {
		t.Fatal(page)
	}
	var deliveries rest.DeliverySummaryPage
	if err := json.Unmarshal(get(base+"/"+receipt.ID+"/deliveries?status=pending", 200), &deliveries); err != nil {
		t.Fatal(err)
	}
	if len(deliveries.Items) != 1 || deliveries.Items[0].Id != id {
		t.Fatal(deliveries)
	}
	get(base+"?cursor=invalid", 400)
	get(base+"/"+receipt.ID+"/deliveries?status=invalid", 400)
	get(base+"/missing/deliveries", 404)
	second, _, _, err := svc.Create(ctx, name, "history-second", intake.InlineRequest{Sender: batch.Sender{Email: "from@example.com"}, Subject: "Second", HTML: "Second", Recipients: []intake.InlineRecipient{{Email: "a@example.com"}, {Email: "b@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE batch_intake_receipts SET created_at=$1 WHERE batch_id=$2`, receipt.CreatedAt, second.ID); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(get(base+"?limit=1", 200), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == nil {
		t.Fatal("missing cursor")
	}
	firstID := page.Items[0].Id
	next := *page.NextCursor
	if err = json.Unmarshal(get(base+"?limit=1&cursor="+next, 200), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Id == firstID || page.NextCursor != nil {
		t.Fatal(page)
	}
	get(base+"/"+receipt.ID+"/deliveries?cursor="+next, 400)
	if err = json.Unmarshal(get(base+"/"+second.ID+"/deliveries?limit=1", 200), &deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries.NextCursor == nil {
		t.Fatal("missing delivery cursor")
	}
	deliveryCursor := *deliveries.NextCursor
	firstDelivery := deliveries.Items[0].Id
	if err = json.Unmarshal(get(base+"/"+second.ID+"/deliveries?limit=1&cursor="+deliveryCursor, 200), &deliveries); err != nil {
		t.Fatal(err)
	}
	if len(deliveries.Items) != 1 || deliveries.Items[0].Id <= firstDelivery || deliveries.NextCursor != nil {
		t.Fatal(deliveries)
	}
	get(base+"/"+second.ID+"/deliveries?status=pending&cursor="+deliveryCursor, 400)
	repo := sqlc.NewDeliveryRepository(db, nil, time.Hour)
	if err := repo.SetScheduled(ctx, batch.ID(receipt.ID), "to@example.com"); err != nil {
		t.Fatal(err)
	}
	if d := read("example.com", receipt.ID, 200); d.Status != "scheduled" {
		t.Fatal(d)
	}
	if _, err := repo.PrepareForSend(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if d := read("example.com", receipt.ID, 200); d.Status != "sending" {
		t.Fatal(d)
	}
	sr := sqlc.NewStatsRepository(db)
	emit := func(o stats.Outcome, at time.Time) {
		t.Helper()
		if err := sr.Insert(ctx, stats.NewStat("to@example.com", receipt.ID, name, at, o)); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	emit(stats.Delivered(), now)
	if err := repo.Clean(ctx, batch.ID(receipt.ID), "to@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, batch.ID(receipt.ID), "to@example.com"); !errors.Is(err, delivery.ErrDeliveryNotFound) {
		t.Fatal(err)
	}
	if d := read("example.com", receipt.ID, 200); d.Status != "delivered" {
		t.Fatal(d)
	}
	var complete rest.Batch
	if err := json.Unmarshal(get(base+"/"+receipt.ID, 200), &complete); err != nil {
		t.Fatal(err)
	}
	if complete.Status != "completed" || complete.CompletedAt == nil || complete.ExpiresAt == nil || complete.ExpiresAt.Sub(*complete.CompletedAt) != 30*24*time.Hour {
		t.Fatal(complete)
	}
	emit(stats.Bounced(true, 550, "gone"), now.Add(time.Second))
	if d := read("example.com", receipt.ID, 200); d.Status != "bounced" || d.LastError == nil || d.LastError.Detail != "gone" {
		t.Fatal(d)
	}
	var bounced rest.Batch
	if err := json.Unmarshal(get(base+"/"+receipt.ID, 200), &bounced); err != nil {
		t.Fatal(err)
	}
	if bounced.DeliveryCounts.Bounced != 1 || !bounced.CompletedAt.Equal(*complete.CompletedAt) || !bounced.ExpiresAt.Equal(*complete.ExpiresAt) {
		t.Fatal(bounced)
	}
	emit(stats.Delivered(), now)
	if d := read("example.com", receipt.ID, 200); d.Status != "bounced" {
		t.Fatal(d)
	}
	emit(stats.Opened("private agent", "192.0.2.1"), now.Add(2*time.Second))
	emit(stats.Clicked("private agent", "192.0.2.1", "https://example.com"), now.Add(3*time.Second))
	if d := read("example.com", receipt.ID, 200); d.Status != "bounced" {
		t.Fatal(d)
	}
	for _, outcome := range []stats.Outcome{stats.Rejected("invalid recipient"), stats.Failed("retry budget spent")} {
		emit(outcome, now.Add(4*time.Second))
		now = now.Add(5 * time.Second)
		if d := read("example.com", receipt.ID, 200); string(d.Status) != string(outcome.Type()) || d.LastError == nil || d.LastError.Detail == "" {
			t.Fatal(d)
		}
	}
	// Concurrent final outcomes must persist one completion fact for mixed results.
	outcomes := []stats.Outcome{stats.Delivered(), stats.Failed("retry budget spent")}
	done := make(chan error, 2)
	for i, email := range []string{"a@example.com", "b@example.com"} {
		go func(email string, o stats.Outcome) {
			done <- sr.Insert(ctx, stats.NewStat(email, second.ID, name, time.Now(), o))
		}(email, outcomes[i])
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var mixed rest.Batch
	if err := json.Unmarshal(get(base+"/"+second.ID, 200), &mixed); err != nil {
		t.Fatal(err)
	}
	if mixed.Status != "completed" || mixed.CompletedAt == nil || mixed.DeliveryCounts.Delivered != 1 || mixed.DeliveryCounts.Failed != 1 {
		t.Fatal(mixed)
	}
	sender := authz.NewContext(t.Context(), authz.MustNewPrincipal("sender", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	if _, err := svc.ReadBatch(sender, name, receipt.ID); err == nil {
		t.Fatal("sender read batch")
	}
	if _, err := svc.ListBatches(sender, name, intake.HistoryQuery{Limit: 100}); err == nil {
		t.Fatal("sender listed batches")
	}
	if _, err := svc.GetDelivery(sender, name, receipt.ID, id); err == nil {
		t.Fatal("sender read history")
	}
	read("other.com", receipt.ID, 404)
	read("example.com", "wrong", 404)
	reader := authz.NewContext(t.Context(), authz.MustNewPrincipal("reader", authz.MustNewGrant(authz.RoleBatchReader, authz.DomainAnchor(name))))
	if _, err := svc.GetDelivery(reader, name, receipt.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetDelivery(reader, values.MustParse("other.com"), receipt.ID, id); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatal(err)
	}
}
