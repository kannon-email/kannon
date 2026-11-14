package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"go.yaml.in/yaml/v3"
)

func TestEveryVersionServesTheSameRoutes(t *testing.T) {
	h := rest.NewHandler(rest.Services{Token: admintoken.MustParse("secret"), Ready: func(context.Context) error { return nil }})
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/v1/health", 200},
		{"/v1beta1/health", 200},
		{"/v1/domains", 401},
		{"/v1beta1/domains", 401},
		{"/v2/health", 404},
		{"/health", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.code {
			t.Errorf("%s: status %d, want %d", tc.path, w.Code, tc.code)
		}
	}
}

func TestDeprecatedContractMirrorsV1(t *testing.T) {
	v1, beta := readContract(t, "openapi.yaml"), readContract(t, "openapi_v1beta1.yaml")
	v1Info, betaInfo := object(t, v1["info"]), object(t, beta["info"])
	if betaInfo["version"] != "v1beta1" || v1Info["version"] != "v1" {
		t.Fatalf("versions: v1 %v, v1beta1 %v", v1Info["version"], betaInfo["version"])
	}
	for _, field := range []string{"version", "description"} {
		betaInfo[field] = v1Info[field]
	}
	beta["servers"], beta["x-compatibility"] = v1["servers"], v1["x-compatibility"]
	for path, item := range object(t, beta["paths"]) {
		for method, op := range object(t, item) {
			operation := object(t, op)
			if operation["deprecated"] != true {
				t.Errorf("%s %s is not deprecated in v1beta1", method, path)
			}
			delete(operation, "deprecated")
		}
	}
	if !reflect.DeepEqual(v1, beta) {
		t.Fatal("v1beta1 contract diverges from v1 beyond version, servers and deprecation")
	}
}

func TestPublishedContractsMatchTheAdapter(t *testing.T) {
	for published, embedded := range map[string]string{"../../../api/openapi/v1.yaml": "openapi.yaml", "../../../api/openapi/v1beta1.yaml": "openapi_v1beta1.yaml"} {
		a, err := os.ReadFile(published)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(embedded)
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs from %s", published, embedded)
		}
	}
}

func readContract(t *testing.T, file string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected an OpenAPI object, got %T", v)
	}
	return m
}
