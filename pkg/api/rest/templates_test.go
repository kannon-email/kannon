package rest_test

import (
	"context"
	"encoding/json"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersistentTemplateJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	db, cleanup, err := credentialDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	defer db.Close()
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping, Keys: apikeys.NewService(sqlc.NewAPIKeysRepository(db)), Templates: templates.NewService(sqlc.NewTemplatesRepository(db))})
	call := func(token, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}
	call("secret", "POST", "/v1beta1/domains", `{"domain":"example.com"}`, 201)
	base := "/v1beta1/domains/example.com/templates"
	for _, body := range []string{`{}`, `{"html":"body"}`, `{"title":"x"}`, `{"title":"x","html":""}`, `{"title":null,"html":"body"}`} {
		call("secret", "POST", base, body, 422)
	}
	created := call("secret", "POST", base, `{"title":"Welcome","html":"<p>Hello</p>"}`, 201)
	var resource rest.Template
	if err := json.Unmarshal(created.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	location := created.Header().Get("Location")
	if location != base+"/"+resource.Id || resource.Lifetime != "persistent" {
		t.Fatal(created)
	}
	for _, body := range []string{`{}`, `{"html":""}`, `{"html":null}`, `{"title":null,"html":"new"}`} {
		call("secret", "PATCH", location, body, 422)
	}
	call("secret", "PATCH", location, `{"title":"Updated"}`, 200)
	read := call("secret", "GET", location, "", 200)
	if err := json.Unmarshal(read.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Title != "Updated" || resource.Html != "<p>Hello</p>" || resource.Domain != "example.com" {
		t.Fatal(resource)
	}
	call("secret", "POST", base, `{"title":"Second","html":"body"}`, 201)
	// A sending-created transient Template must never appear in authored lists.
	domain := values.MustParse("example.com")
	transient, err := templates.NewTransient(domain, "private sending body")
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlc.NewTemplatesRepository(db).Create(context.Background(), transient); err != nil {
		t.Fatal(err)
	}
	list := call("secret", "GET", base+"?limit=1", "", 200)
	if strings.Contains(list.Body.String(), "html") {
		t.Fatal("HTML in list")
	}
	var page rest.TemplateSummaryPage
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal(page)
	}
	next := call("secret", "GET", base+"?cursor="+*page.NextCursor, "", 200)
	var second rest.TemplateSummaryPage
	if err := json.Unmarshal(next.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Id <= page.Items[0].Id {
		t.Fatal(second)
	}
	call("secret", "GET", "/v1beta1/domains/other.com/templates?cursor="+*page.NextCursor, "", 400)
	for _, role := range []string{"admin", "sender"} {
		key := call("secret", "POST", "/v1beta1/domains/example.com/api-keys", `{"name":"test","roles":["`+role+`"]}`, 201)
		var k rest.APIKeyCreated
		if err := json.Unmarshal(key.Body.Bytes(), &k); err != nil {
			t.Fatal(err)
		}
		status := 403
		if role == "admin" {
			status = 200
		}
		call(k.Secret, "GET", base, "", status)
		call(k.Secret, "GET", "/v1beta1/domains/other.com/templates", "", 403)
		call(k.Secret, "POST", "/v1beta1/domains/other.com/templates", `{"title":"x","html":"body"}`, 403)
		if role == "admin" {
			own := call(k.Secret, "POST", base, `{"title":"local","html":"local body"}`, 201)
			ownLocation := own.Header().Get("Location")
			call(k.Secret, "PATCH", ownLocation, `{"html":"updated local body"}`, 200)
			call(k.Secret, "DELETE", ownLocation, "", 204)
		}
		for _, method := range []string{"GET", "PATCH", "DELETE"} {
			call(k.Secret, method, "/v1beta1/domains/other.com/templates/"+resource.Id, `{"title":"x"}`, 403)
		}
		if role == "sender" {
			for _, method := range []string{"GET", "PATCH", "DELETE"} {
				call(k.Secret, method, location, `{"title":"x"}`, 403)
			}
			call(k.Secret, "POST", base, `{"title":"x","html":"x"}`, 403)
		}
	}
	// Exercise the application seam over the same real persistence as the HTTP adapter.
	application := templates.NewService(sqlc.NewTemplatesRepository(db))
	ctx := authz.NewContext(context.Background(), authz.MustNewPrincipal("domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(domain))))
	html := "<p>Changed</p>"
	patched, err := application.PatchTemplate(ctx, domain, resource.Id, &html, nil)
	if err != nil || patched.Title() != "Updated" || patched.Html() != html {
		t.Fatal(patched, err)
	}
	read = call("secret", "GET", location, "", 200)
	if err := json.Unmarshal(read.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Html != html {
		t.Fatal(resource)
	}
	call("secret", "DELETE", location, "", 204)
	call("secret", "GET", location, "", 404)
}
