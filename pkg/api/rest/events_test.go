package rest_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
)

func TestPersistentEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	db, purge, err := credentialDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		if err := purge(); err != nil {
			t.Error(err)
		}
	}()
	svc := stats.NewService(sqlc.NewStatsRepository(db))
	d := values.MustParse("events.example")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, s := range []*stats.Stat{
		stats.NewStat("a@example.com", "old-batch", d, start, stats.Delivered()),
		stats.NewStat("b@example.com", "old-batch", d, start, stats.Rejected("invalid")),
		stats.NewStat("c@example.com", "old-batch", d, start.Add(time.Hour), stats.Delivered()),
		stats.NewStat("anonymous@track.events.example", "old-batch", d, start, stats.Opened("", "")),
		stats.NewStat("a@example.com", "old-batch", d, start, stats.Errored(451, "retry")),
	} {
		if err := svc.InsertStat(t.Context(), s); err != nil {
			t.Fatal(err)
		}
	}

	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("test", authz.MustNewGrant(authz.RoleAnalyst, authz.DomainAnchor(d))))
	appPage, err := svc.QueryEvents(ctx, d, stats.EventQuery{Range: stats.TimeRange{Start: start, Stop: start.Add(time.Hour)}, Types: []stats.Type{stats.TypeDelivered}, Limit: 10})
	if err != nil || len(appPage.Items) != 1 || appPage.Items[0].MessageID != "old-batch" {
		t.Fatal(appPage, err)
	}
	keys := apikeys.NewService(sqlc.NewAPIKeysRepository(db))
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping, Stats: svc, Keys: keys})
	call := func(q string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/events.example/stats/events?"+q, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	rDomain := httptest.NewRequest(http.MethodPost, "/v1beta1/domains", strings.NewReader(`{"domain":"events.example"}`))
	rDomain.Header.Set("Authorization", "Bearer secret")
	wDomain := httptest.NewRecorder()
	h.ServeHTTP(wDomain, rDomain)
	if wDomain.Code != 201 {
		t.Fatal(wDomain.Body)
	}
	q := "from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&limit=1"
	page := call(q, 200)
	items, ok := page["items"].([]any)
	if !ok {
		t.Fatal(page)
	}
	if len(items) != 1 {
		t.Fatal(items)
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["type"] != "rejected" {
		t.Fatal(page)
	}
	page = call(q+"&cursor="+fmt.Sprint(page["next_cursor"]), 200)
	items, ok = page["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal(page)
	}
	item, ok = items[0].(map[string]any)
	if !ok || item["type"] != "delivered" || page["next_cursor"] != nil {
		t.Fatal(page)
	}
	cursor := fmt.Sprint(call(q, 200)["next_cursor"])
	call(q+"&type=delivered&cursor="+cursor, 400)
	filteredCursor := fmt.Sprint(call(q+"&type=delivered&type=rejected", 200)["next_cursor"])
	call(strings.Replace(q, "limit=1", "limit=100", 1)+"&type=rejected&type=delivered&type=rejected&cursor="+filteredCursor, 200)
	call(q+"&extra=x", 400)
	call(q+"&type=error", 400)
	call("from=2026-01-01T00:00:00&to=2026-01-02T00:00:00Z", 400)
	call(q+"&from=2026-01-01T00:00:00Z", 400)
	mixed := call("from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&type=delivered&type=rejected", 200)
	items, ok = mixed["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatal(mixed)
	}
	page = call(q+"&type=delivered", 200)
	items, ok = page["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal(page)
	}
	for _, role := range []string{"analyst", "metrics-reader", "sender", "admin"} {
		r := httptest.NewRequest(http.MethodPost, "/v1beta1/domains/events.example/api-keys", strings.NewReader(`{"name":"reader","roles":["`+role+`"]}`))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatal(w.Body)
		}
		var key rest.APIKeyCreated
		if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
			t.Fatal(err)
		}
		for _, domain := range []string{"events.example", "other.example"} {
			r = httptest.NewRequest(http.MethodGet, "/v1beta1/domains/"+domain+"/stats/events?"+q, nil)
			r.Header.Set("Authorization", "Bearer "+key.Secret)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 403
			if domain == "events.example" && (role == "analyst" || role == "admin") {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("role %s domain %s: %d %s", role, domain, w.Code, w.Body)
			}
		}
	}
	for _, mode := range []tracking.Mode{tracking.ModePseudonymous, tracking.ModeIdentified, tracking.ModeFull} {
		email := string(mode) + "@example.com"
		if mode == tracking.ModePseudonymous {
			email = "0123456789abcdef0123456789abcdef@track.events.example"
		}
		st := stats.NewStat(email, "privacy-batch", d, start, stats.Clicked("browser", "192.0.2.1", "https://example.com"))
		st.TrackingMode = mode
		if err := svc.InsertStat(t.Context(), st); err != nil {
			t.Fatal(err)
		}
	}
	page = call("from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&batch_id=privacy-batch", 200)
	rows, ok := page["items"].([]any)
	if !ok {
		t.Fatal(page)
	}
	if len(rows) != 3 {
		t.Fatal(page)
	}
	for _, item := range rows {
		v, ok := item.(map[string]any)
		if !ok {
			t.Fatal(item)
		}
		data, ok := v["data"].(map[string]any)
		if !ok {
			t.Fatal(v)
		}
		mode := v["tracking_mode"]
		if mode != "full" && (data["ip"] != nil || data["user_agent"] != nil) {
			t.Fatal(v)
		}
		if mode == "pseudonymous" && (v["email"] != nil || v["delivery_id"] != nil || v["tracking_identity"] == nil) {
			t.Fatal(v)
		}
	}
}
