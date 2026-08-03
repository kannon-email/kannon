package mailapi

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/values"
)

// senderKeyFor is the Principal an API Key of that Domain resolves to: sender, one Grant, anchored
// there. Built here rather than imported so this table exercises the decision and not the adapter,
// which internal/apikeys/principal_test.go covers.
func senderKeyFor(tenant values.DomainName) authz.Principal {
	return authz.MustNewPrincipal("key_test@"+tenant.String(),
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(tenant)))
}

// TestSenderHostPermitted holds the sender-domain rule to exactly what it permitted before the
// guard replaced the explicit tenant comparison: every case of the old table, unchanged, now run
// against the authority model. A From host that is the Domain or a proper parent of it, nothing else.
func TestSenderHostPermitted(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		tenant string
		want   bool
	}{
		{"exact match", "example.com", "example.com", true},
		{"case insensitive", "Example.COM", "example.com", true},
		{"trailing dot tolerated", "example.com.", "example.com", true},
		{"parent allowed", "example.com", "k.example.com", true},
		{"grandparent allowed", "example.com", "a.b.example.com", true},
		{"child of tenant rejected", "sub.example.com", "example.com", false},
		{"sibling rejected", "evil.com", "example.com", false},
		{"lookalike suffix rejected", "ample.com", "example.com", false},
		{"prefix substring rejected", "example.co", "example.com", false},
		{"empty from rejected", "", "example.com", false},

		// A host that cannot be a canonical domain name is refused by the same guard, not by a
		// check of its own: it can be neither the caller's Domain nor a parent of one. The first
		// two carry the Resource tree's own punctuation, which canonical form keeps out (ADR 0008).
		{"path separator in from rejected", "example.com/batches", "example.com", false},
		{"wildcard in from rejected", "*.example.com", "example.com", false},
		{"single label from rejected", "localhost", "example.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tenant := values.MustParse(tc.tenant)
			got := authz.Can(senderKeyFor(tenant), authz.Create,
				senderBatches(canonicalSenderHost(tc.from), tenant))
			if got != tc.want {
				t.Fatalf("sending from %q as tenant %q = %v, want %v", tc.from, tc.tenant, got, tc.want)
			}
		})
	}
}

// The parent-domain allowance widens which host may appear in From and must never widen who may
// send, so it is still answered by the guard rather than around it. The comparison it replaces
// consulted no authority at all, so any authenticated caller got the allowance.
func TestParentAllowanceStillRequiresSendingAuthority(t *testing.T) {
	tenant := values.MustParse("k.example.com")
	parent := canonicalSenderHost("example.com")

	stranger := senderKeyFor(values.MustParse("other.example.com"))
	if authz.Can(stranger, authz.Create, senderBatches(parent, tenant)) {
		t.Fatal("a key of another Domain must not send from this tenant's parent")
	}

	revoked := authz.MustNewPrincipal("key_revoked@k.example.com")
	if authz.Can(revoked, authz.Create, senderBatches(parent, tenant)) {
		t.Fatal("a Principal with no Grants must not send from the parent either")
	}
}

// senderDomainAllowedBefore is the rule as it stood before the guard replaced it, kept verbatim as
// the oracle the differential below compares against. The one copy of the old comparison left in
// the tree, and it exists only to be disagreed with under conditions no request can reach.
func senderDomainAllowedBefore(fromDomain, tenantDomain string) bool {
	from := strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	tenant := strings.ToLower(strings.TrimSuffix(tenantDomain, "."))
	if from == "" || tenant == "" {
		return false
	}
	if from == tenant {
		return true
	}
	return strings.HasSuffix(tenant, "."+from)
}

// TestSenderRuleIsUnchangedFromTheExplicitComparison compares the new decision against the old one
// over every host either might disagree about, since narrowing what a customer may send as would be
// as much a regression as widening it. Both disagreement classes are unreachable past validation.
func TestSenderRuleIsUnchangedFromTheExplicitComparison(t *testing.T) {
	tenants := []string{
		"example.com", "k.example.com", "a.b.example.com", "test.co.uk",
		"x-y.example.com", "a_b.example.com", "sub.batches.com", "1.2.3.com",
	}
	hosts := []string{
		"example.com", "EXAMPLE.COM", "example.com.", "Example.Com.", "EXAMPLE.COM.",
		"k.example.com", "a.b.example.com", "b.example.com", "com", "com.", "co.uk",
		"uk", "test.co.uk", "ample.com", "example.co", "sub.example.com", "evil.com",
		"notexample.com", "", ".", "..", "example..com", ".example.com", "example.com..",
		"example.com/batches", "*.example.com", "localhost", "batches", "stats",
		"x-y.example.com", "a_b.example.com", "3.com", "  example.com  ", "e@x.com",
		strings.Repeat("a", 250) + ".com", strings.Repeat("a", 260) + ".com",
	}

	for _, ts := range tenants {
		tenant := values.MustParse(ts)
		key := senderKeyFor(tenant)
		for _, host := range hosts {
			before := senderDomainAllowedBefore(host, ts)
			now := authz.Can(key, authz.Create, senderBatches(canonicalSenderHost(host), tenant))
			if before == now {
				continue
			}
			if smtputils.Validate("a@" + host) {
				t.Errorf("who may send as what changed: tenant %q, host %q was %v and is now %v",
					ts, host, before, now)
				continue
			}
			t.Logf("tolerated difference on an address that never reaches the rule: tenant %q, host %q was %v and is now %v",
				ts, host, before, now)
		}
	}
}

// The zero Name is nobody's parent and nobody's child, so a From host that could not be
// canonicalised reaches the guard's refusal rather than the shorter path it would compose. This is
// the old table's "empty from"/"empty tenant" pair, stated where those values can be constructed.
func TestZeroDomainNameIsNobodysParent(t *testing.T) {
	real := values.MustParse("k.example.com")

	if isParentDomain(values.DomainName{}, real) {
		t.Fatal("an unresolvable From host must not be treated as a parent")
	}
	if isParentDomain(real, values.DomainName{}) {
		t.Fatal("no host is a parent of an unset Domain")
	}
	if isParentDomain(values.DomainName{}, values.DomainName{}) {
		t.Fatal("the zero Name is not its own parent")
	}
}
