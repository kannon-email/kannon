package apimetrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/pkg/api/apimetrics"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const adminToken = "0123456789abcdef0123456789abcdef"

type sample struct {
	api, operation, code string
}

func TestRESTOperationIsTheRoutePatternNeverThePath(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))

	for _, path := range []string{"/v1/domains/example.com", "/v1beta1/domains/example.com"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, path, strings.NewReader("{}")))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", path, w.Code)
		}
	}

	assertCounted(t, reg, sample{"rest", "PATCH /domains/{domain}", "4xx"}, 2)
	assertNoLabelValueContains(t, reg, "example.com")
}

func TestRESTCountsAnAuthenticatedRouteByItsInnerPattern(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))

	req := httptest.NewRequest(http.MethodGet, "/v1/domains?wat=1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	h.ServeHTTP(httptest.NewRecorder(), req)

	assertCounted(t, reg, sample{"rest", "GET /domains", "4xx"}, 1)
}

func TestRESTCountsAnUnmatchedRequestAsUnmatched(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))

	for _, path := range []string{"/nowhere/example.com", "/v1/nowhere/example.com"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	assertCounted(t, reg, sample{"rest", "unmatched", "4xx"}, 2)
	assertNoLabelValueContains(t, reg, "example.com")
}

func TestRESTObservesTheDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil))

	for _, m := range family(t, reg, "kannon_api_request_duration_seconds").GetMetric() {
		if labels(m) == (sample{api: "rest", operation: "GET /v1/openapi.yaml"}) && m.GetHistogram().GetSampleCount() == 1 {
			return
		}
	}
	t.Fatal("expected one duration observed for GET /v1/openapi.yaml")
}

func TestRegisteringTwiceOnOneRegistryReusesTheCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	first := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))
	second := apimetrics.New(reg).REST(rest.NewHandler(rest.Services{Token: admintoken.MustParse(adminToken)}))

	first.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil))
	second.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil))

	assertCounted(t, reg, sample{"rest", "GET /v1/openapi.yaml", "2xx"}, 2)
}

func family(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	t.Fatalf("no %s gathered", name)
	return nil
}

func labels(m *dto.Metric) sample {
	var s sample
	for _, l := range m.GetLabel() {
		switch l.GetName() {
		case "api":
			s.api = l.GetValue()
		case "operation":
			s.operation = l.GetValue()
		case "code":
			s.code = l.GetValue()
		}
	}
	return s
}

func assertCounted(t *testing.T, reg *prometheus.Registry, want sample, count float64) {
	t.Helper()
	var seen []sample
	for _, m := range family(t, reg, "kannon_api_requests_total").GetMetric() {
		if labels(m) == want {
			if got := m.GetCounter().GetValue(); got != count {
				t.Fatalf("%+v counted %v, want %v", want, got, count)
			}
			return
		}
		seen = append(seen, labels(m))
	}
	t.Fatalf("no %+v counted; saw %+v", want, seen)
}

func assertNoLabelValueContains(t *testing.T, reg *prometheus.Registry, needle string) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "domain" || strings.Contains(l.GetValue(), needle) {
					t.Errorf("%s carries %s=%q", f.GetName(), l.GetName(), l.GetValue())
				}
			}
		}
	}
}
