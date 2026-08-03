// This test is in package templates rather than templates_test so that it can
// exercise newTemplateID directly. The property under test is that two functions
// agree, and reaching only the exported half would leave the composition side
// asserted by inference.
package templates

import (
	"testing"

	"github.com/kannon-email/kannon/internal/values"
)

// The two halves of the id format have to agree, and this is what makes them: a
// change to the composition that missed the parse — or a new separator, or a prefix
// with an "@" in it — fails here rather than in an authorization decision, which is
// where it would otherwise be discovered.
func TestDomainFromIDRoundTripsNewTemplateID(t *testing.T) {
	for _, raw := range []string{
		"example.com",
		"a.b.co.uk",
		"sub.domain-with-dash.io",
		"under_score.example.com",
		// Canonicalisation happens on the way in, so the id already carries the
		// lower-cased form and the round trip is unaffected by how it was spelled.
		"EXAMPLE.COM",
	} {
		t.Run(raw, func(t *testing.T) {
			domain := values.MustParse(raw)

			got, err := DomainFromID(newTemplateID(domain))
			if err != nil {
				t.Fatalf("DomainFromID: %v", err)
			}
			if got != domain {
				t.Errorf("recovered %q, composed from %q", got, domain)
			}
		})
	}
}

// The same, through the constructors a caller actually uses: whatever id a Template
// is created with must be one this parse can take apart, or the three Admin API
// operations that carry no Domain cannot serve a Template that exists.
func TestDomainFromIDRoundTripsCreatedTemplates(t *testing.T) {
	domain := values.MustParse("example.com")

	persistent, err := NewPersistent(domain, "<p>hi</p>", "hi")
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	transient, err := NewTransient(domain, "<p>hi</p>")
	if err != nil {
		t.Fatalf("NewTransient: %v", err)
	}

	for _, tpl := range []*Template{persistent, transient} {
		got, err := DomainFromID(tpl.TemplateID())
		if err != nil {
			t.Fatalf("DomainFromID(%q): %v", tpl.TemplateID(), err)
		}
		if got != tpl.DomainName() {
			t.Errorf("recovered %q from %q, want %q", got, tpl.TemplateID(), tpl.DomainName())
		}
	}
}

// Everything this refuses, it refuses because accepting it would let one id mean two
// Domains — one to the guard and another to the load. The zero FQDN is asserted
// alongside the error because a caller that ignored the error would otherwise get a
// value that renders an unauthorizable Resource and a refusal several frames from
// the cause.
func TestDomainFromIDRefusesWhatCarriesNoSingleDomain(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{
			// No separator: nothing in the string is a Domain. Returning "" here is
			// what the old code would have done, and it would have authorized
			// against a Resource nothing covers.
			name: "no separator at all",
			id:   "template_ckv0d2n",
		},
		{
			// The hazard ADR 0008 names. A lenient split takes one of these and the
			// load takes the other, so the caller is authorized for a.com and served
			// from b.com. Refusing the ambiguity is cheaper than deciding it.
			name: "two separators",
			id:   "template_ckv0d2n@a.com@b.com",
		},
		{
			name: "three separators",
			id:   "template_ckv0d2n@a.com@b.com@c.com",
		},
		{
			name: "empty domain",
			id:   "template_ckv0d2n@",
		},
		{
			// "templates", "apikeys", "batches" and "stats" are all valid hostnames
			// and also segments of the Resource tree, so a single-label Domain could
			// alias another node of it. The dot rule is what removes the class.
			name: "single-label domain",
			id:   "template_ckv0d2n@templates",
		},
		{
			name: "single-label domain that is not a tree segment either",
			id:   "template_ckv0d2n@localhost",
		},
		{
			// A path separator inside what would become one segment of a Resource
			// path. values.Parse refuses it, which is why this parse does not have to
			// know anything about the Resource tree.
			name: "a path separator in the domain",
			id:   "template_ckv0d2n@a.com/templates",
		},
		{
			name: "a wildcard in the domain",
			id:   "template_ckv0d2n@*",
		},
		{
			name: "nothing but the separator",
			id:   "@",
		},
		{
			name: "empty id",
			id:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DomainFromID(tc.id)
			if err == nil {
				t.Fatalf("DomainFromID(%q) = %q, want an error", tc.id, got)
			}
			if !got.IsZero() {
				t.Errorf("DomainFromID(%q) returned %q alongside its error", tc.id, got)
			}
		})
	}
}

// An id nobody composed, whose Domain differs only in case, resolves to the
// canonical Domain — and that is safe rather than a normalisation the authority
// model forbids. The lower-casing happens here, at the edge, in values.Parse; the
// value the guard is given is then the value the load is given, so the two cannot
// disagree. What ADR 0008 forbids is normalising *inside* the decision, where a
// Grant on TEST.com could be handed another Domain's data.
func TestDomainFromIDCanonicalisesTheDomainItRecovers(t *testing.T) {
	got, err := DomainFromID("template_ckv0d2n@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("DomainFromID: %v", err)
	}
	if want := values.MustParse("example.com"); got != want {
		t.Errorf("recovered %q, want %q", got, want)
	}
}
