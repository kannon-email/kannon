package rest_test

import (
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAggregateRoute(t *testing.T) {
	h := rest.NewHandlerWithStats(nil, admintoken.MustParse("secret"), nil, stats.NewService(stats.NewInMemRepository(), stats.WithAggregatedStatsRepository(stats.NewInMemAggregatedStatsRepository())))
	r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains/example.com/stats/aggregated?from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&group_by=1h", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
