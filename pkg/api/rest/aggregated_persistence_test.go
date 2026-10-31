package rest_test

import (
	"encoding/json"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPersistentAggregateSeries(t *testing.T) {
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
	svc := stats.NewService(sqlc.NewStatsRepository(db), stats.WithAggregatedStatsRepository(sqlc.NewAggregatedStatsRepository(db)))
	d := values.MustParse("aggregate.example")
	start := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	// Anonymous engagement contributes counters without any recipient event row.
	for _, at := range []time.Time{start.Add(-time.Hour), start, start.Add(time.Hour), start.Add(time.Hour), start.Add(6 * time.Hour)} {
		if err := svc.IncrementAggregatedStat(t.Context(), d, at, stats.TypeOpened); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.IncrementAggregatedStat(t.Context(), d, start, stats.TypeAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cleanup(t.Context(), time.Hour); err != nil {
		t.Fatal(err)
	}
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("reader", authz.MustNewGrant(authz.RoleMetricsReader, authz.DomainAnchor(d))))
	buckets, err := svc.QueryAggregateSeries(ctx, d, stats.TimeRange{Start: start, Stop: start.Add(6 * time.Hour)}, 3*time.Hour)
	if err != nil || len(buckets) != 2 || buckets[0].Counts[stats.TypeOpened] != 3 || buckets[1].Counts[stats.TypeOpened] != 0 {
		t.Fatal(buckets, err)
	}
	keys := apikeys.NewService(sqlc.NewAPIKeysRepository(db))
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping, Stats: svc, Keys: keys})
	call := func(token, method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		return w.Body.String()
	}
	call("secret", "POST", "/v1beta1/domains", `{"domain":"aggregate.example"}`, 201)
	base := "/v1beta1/domains/aggregate.example/stats/aggregated?"
	q := "from=2026-01-01T03:00:00Z&to=2026-01-01T09:00:00Z&group_by=3h"
	body := call("secret", "GET", base+q, "", 200)
	shifted := strings.ReplaceAll(strings.ReplaceAll(q, "03:00:00Z", "04:00:00%2B01:00"), "09:00:00Z", "10:00:00%2B01:00")
	var offsetResult map[string]any
	var utcResult map[string]any
	if err := json.Unmarshal([]byte(call("secret", "GET", base+shifted, "", 200)), &offsetResult); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(body), &utcResult); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(offsetResult, utcResult) {
		t.Fatal(offsetResult, utcResult)
	}
	var got struct {
		Buckets []struct {
			Start  time.Time
			Counts map[string]int64
		}
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Buckets) != 2 || got.Buckets[0].Counts["opened"] != 3 || got.Buckets[0].Counts["validated"] != 1 || len(got.Buckets[1].Counts) != 7 || got.Buckets[1].Counts["opened"] != 0 || !got.Buckets[1].Start.Equal(start.Add(3*time.Hour)) {
		t.Fatal(body)
	}
	for _, query := range []string{
		"from=2026-01-01T03:00:00Z&to=2026-01-01T09:00:00Z&group_by=1h",
		"from=2026-01-01T03:00:00Z&to=2026-01-02T03:00:00Z&group_by=24h",
		"from=2026-01-01T03:00:00Z&to=2026-02-11T19:00:00Z&group_by=1h",
	} {
		call("secret", "GET", base+query, "", 200)
	}
	for _, query := range []string{
		strings.Replace(q, "3h", "30m", 1), strings.Replace(q, "3h", "1h30m", 1), strings.Replace(q, "3h", "0h", 1), strings.Replace(q, "3h", "-1h", 1),
		strings.Replace(q, "03:00:00Z", "03:30:00Z", 1), strings.Replace(q, "09:00:00Z", "08:00:00Z", 1), strings.Replace(q, "09:00:00Z", "03:00:00Z", 1),
		"from=2026-01-01T03:00:00Z&to=2026-02-11T20:00:00Z&group_by=1h", "group_by=1h", q + "&group_by=1h", q + "&batch_id=old", q + "&limit=10", strings.Replace(q, "03:00:00Z", "03:00:00", 1),
	} {
		call("secret", "GET", base+query, "", 400)
	}
	for _, role := range []string{"admin", "analyst", "metrics-reader", "sender", "batch-reader"} {
		var key rest.APIKeyCreated
		if err := json.Unmarshal([]byte(call("secret", "POST", "/v1beta1/domains/aggregate.example/api-keys", `{"name":"reader","roles":["`+role+`"]}`, 201)), &key); err != nil {
			t.Fatal(err)
		}
		want := 200
		if role == "sender" || role == "batch-reader" {
			want = 403
		}
		call(key.Secret, "GET", base+q, "", want)
		call(key.Secret, "GET", strings.Replace(base, "aggregate.example", "other.example", 1)+q, "", 403)
		if role == "metrics-reader" {
			call(key.Secret, "GET", "/v1beta1/domains/aggregate.example/stats/events?from=2026-01-01T03:00:00Z&to=2026-01-01T09:00:00Z", "", 403)
		}
	}
}
