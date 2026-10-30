package rest_test

import (
	"encoding/json"
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
	receipt, _, _, err := svc.Create(ctx, name, "history", intake.InlineRequest{Sender: batch.Sender{Email: "from@example.com"}, Subject: "Hello", HTML: "Hello", Recipients: []string{"to@example.com", "refused"}})
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
		r := httptest.NewRequest("GET", "/v1beta1/domains/"+domain+"/batches/"+bid+"/deliveries/"+id, nil)
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
	if _, err := repo.Get(ctx, batch.ID(receipt.ID), "to@example.com"); err != delivery.ErrDeliveryNotFound {
		t.Fatal(err)
	}
	if d := read("example.com", receipt.ID, 200); d.Status != "delivered" {
		t.Fatal(d)
	}
	emit(stats.Bounced(true, 550, "gone"), now.Add(time.Second))
	if d := read("example.com", receipt.ID, 200); d.Status != "bounced" || d.LastError == nil || d.LastError.Detail != "gone" {
		t.Fatal(d)
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
	sender := authz.NewContext(t.Context(), authz.MustNewPrincipal("sender", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
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
