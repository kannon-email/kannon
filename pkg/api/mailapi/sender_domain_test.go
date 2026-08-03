package mailapi

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/values"
)

// senderKeyFor is the Principal an API Key of that Domain resolves to: sender, one
// Grant, anchored there. Built here rather than imported from internal/apikeys so
// that this table exercises the decision and not the adapter — what the adapter
// produces is asserted in internal/apikeys/principal_test.go.
func senderKeyFor(tenant values.DomainName) authz.Principal {
	return authz.MustNewPrincipal("key_test@"+tenant.String(),
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(tenant)))
}

// TestSenderHostPermitted holds the sender-domain rule to exactly what it permitted
// before the guard replaced the explicit tenant comparison.
//
// Every case of the table this grew out of is here with its name and expectation
// unchanged; what runs underneath is now the authority model — the composed Resource
// and authz.Can — rather than a string comparison. The rule permits a From host that
// *is* the authenticated Domain or is a proper parent of it, and nothing else.
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

		// A host that cannot be a canonical FQDN is refused by the same guard, and
		// not by a check of its own: it can be neither the caller's Domain nor a
		// parent of one, since both are canonical by construction. The first two
		// carry the Resource tree's own punctuation, which is exactly what the
		// canonical form exists to keep out of a path segment (ADR 0008).
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

// The parent-domain allowance widens which host may appear in From and must never
// widen who may send, so it is still answered by the guard rather than around it.
//
// This is what the comparison it replaces could not do: reached only after
// authentication, it consulted no authority at all, so any authenticated caller got
// the allowance. Now a Principal anchored elsewhere, or holding nothing, is refused
// for the parent host exactly as it is for the tenant's own.
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

// senderDomainAllowedBefore is the rule as it stood before the guard replaced it,
// kept verbatim as the oracle the differential below compares against. It is the one
// copy of the old comparison left in the tree, and it exists only to be disagreed
// with under conditions no request can reach.
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

// TestSenderRuleIsUnchangedFromTheExplicitComparison is the differential that
// justifies the claim this change rests on: who may send as what did not move.
//
// The table above states the rule; this compares the new decision against the old
// comparison over every host either might disagree about, and fails on any
// disagreement a request could actually provoke. Silently narrowing what a customer
// may send as would be as much a regression as widening it, and a table of chosen
// cases cannot rule out either — an oracle can.
//
// Two classes of disagreement exist and both are unreachable, because a From host is
// read out of an email address that smtputils.Validate has already accepted:
//
//   - The tenant's last label on its own — "com" for k.example.com — was a permitted
//     parent under the old string suffix rule and is refused now, since a canonical
//     FQDN must carry a dot (ADR 0008: "batches", "stats" and "apikeys" are all
//     valid single-label hostnames and also segments of the Resource tree). No
//     address of the form "a@com" passes validation, so nothing could ever send as
//     one. This one is worth knowing about rather than merely excluding: were
//     validation relaxed, this is the case whose answer would change, and it would
//     change to the safe one.
//   - A host padded with whitespace, which the new canonicalisation trims and the old
//     comparison did not. Validation rejects whitespace anywhere in an address.
//
// So the test asserts the classification and not merely the count: a future change
// that made either class reachable, or that introduced a third, fails here.
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

// The zero FQDN is nobody's parent and nobody's child, so a From host that could not
// be canonicalised — and a Domain that somehow arrived unset — reach the guard's
// refusal rather than falling back on the shorter path either would compose.
//
// This is the "empty from" and "empty tenant" pair of the table this file grew out
// of, stated where those values can still be constructed: a Principal anchored on a
// zero FQDN cannot exist at all, because NewGrant refuses an Anchor with an empty
// segment.
func TestZeroDomainNameIsNobodysParent(t *testing.T) {
	real := values.MustParse("k.example.com")

	if isParentDomain(values.DomainName{}, real) {
		t.Fatal("an unresolvable From host must not be treated as a parent")
	}
	if isParentDomain(real, values.DomainName{}) {
		t.Fatal("no host is a parent of an unset Domain")
	}
	if isParentDomain(values.DomainName{}, values.DomainName{}) {
		t.Fatal("the zero FQDN is not its own parent")
	}
}
