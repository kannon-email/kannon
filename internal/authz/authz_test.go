// Package authz_test exercises the authority model from outside the package, so every assertion
// is one an eventual caller could make. ADR 0008 records why this table is the seam: coverage
// cannot be tested through the API while a synthetic admin Principal is everywhere.
package authz_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two multi-label Domains. Multi-label because internal/values requires a dot: a
// single-label name could equal a segment of the Resource tree and alias it.
var (
	example = values.MustParse("example.com")
	other   = values.MustParse("other.com")
)

// resourceActions is the closed vocabulary minus Attribute, spelled out here
// rather than borrowed from the package so that adding an Action to the model
// does not silently widen what these sweeps assert. Attribute is left out
// because it acts on nothing of Kannon's: it has its own table below.
var resourceActions = []authz.Action{
	authz.Create,
	authz.Read,
	authz.List,
	authz.Update,
	authz.Delete,
}

func principalWith(grants ...authz.Grant) authz.Principal {
	return authz.MustNewPrincipal("key-1@example.com", grants...)
}

func adminOn(a authz.Anchor) authz.Principal {
	return principalWith(authz.MustNewGrant(authz.RoleAdmin, a))
}

func senderOn(a authz.Anchor) authz.Principal {
	return principalWith(authz.MustNewGrant(authz.RoleSender, a))
}

// decision is one row of the tables below: given this Principal, may it perform
// this Action on this Resource?
type decision struct {
	name      string
	principal authz.Principal
	action    authz.Action
	resource  authz.Resource
	want      bool
}

func runDecisions(t *testing.T, tests []decision) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := authz.Can(tc.principal, tc.action, tc.resource)
			assert.Equal(t, tc.want, got, "%s asked %s on %s", tc.principal, tc.action, tc.resource)
		})
	}
}

// everyResource is one Resource of every shape in the tree, for both Domains, so
// that a sweep cannot miss a kind.
func everyResource() []authz.Resource {
	resources := []authz.Resource{authz.Domains()}
	for _, f := range []values.DomainName{example, other} {
		resources = append(resources,
			authz.Domain(f),
			authz.Batches(f),
			authz.Templates(f),
			authz.Template(f, "welcome"),
			authz.APIKeys(f),
			authz.APIKey(f, "key-1"),
			authz.Stats(f),
			authz.AggregatedStats(f),
		)
	}
	return resources
}

// everyGrant is every Grant the seeded catalogue and the two grantable Anchor kinds produce.
// The tables consuming it are negative sweeps, which pass for the wrong reason if it goes
// vacuous — so every Role must yield a Grant, and a pure-shape Role one on every kind.
func everyGrant(t *testing.T) []authz.Grant {
	t.Helper()

	anchors := []authz.Anchor{
		authz.RootAnchor(),
		authz.AllDomainsAnchor(),
		authz.DomainAnchor(example),
		authz.DomainAnchor(other),
	}

	var grants []authz.Grant
	for _, name := range authz.RoleNames() {
		anchored := 0
		for _, a := range anchors {
			g, err := authz.NewGrant(name, a)
			if err != nil {
				continue
			}
			anchored++
			grants = append(grants, g)
		}
		require.NotZero(t, anchored,
			"role %q is grantable on no Anchor, so every sweep below would skip it", name)

		if name == authz.RoleAdmin {
			require.Len(t, anchors, anchored,
				"admin names no kind, so it must be grantable on every grantable Anchor")
		}
	}

	require.NotEmpty(t, grants)
	return grants
}

// Sending mail is create on a Domain's Batches — no send Action, because a Batch is what one
// Mailer API call creates. This is what an API Key resolves to, and "and nothing else" is the
// load-bearing half: the rule pins the kind and the Anchor pins the place.
func TestSendIsCreateOnADomainsBatches(t *testing.T) {
	own := senderOn(authz.DomainAnchor(example))

	runDecisions(t, []decision{
		{"sends for its own Domain", own, authz.Create, authz.Batches(example), true},
		{"cannot send for another Domain", own, authz.Create, authz.Batches(other), false},
		{"cannot read its own Domain", own, authz.Read, authz.Domain(example), false},
		{"cannot read back the Batches it creates", own, authz.Read, authz.Batches(example), false},
		{"cannot change the Tracking Policy", own, authz.Update, authz.Domain(example), false},
		{"cannot rewrite a Template", own, authz.Update, authz.Template(example, "welcome"), false},
		{"cannot mint an API Key", own, authz.Create, authz.APIKeys(example), false},
		{"cannot read statistics", own, authz.Read, authz.Stats(example), false},
		{"cannot read the counters", own, authz.Read, authz.AggregatedStats(example), false},
		{"cannot create a Domain", own, authz.Create, authz.Domains(), false},
		{"cannot list Domains", own, authz.List, authz.Domains(), false},
		{"holds no attribute", own, authz.Attribute, authz.Batches(example), false},
	})
}

// The same Role on the every-Domain Anchor: further reach, not one extra Action.
func TestSenderOnEveryDomainSendsEverywhereAndDoesNothingElse(t *testing.T) {
	all := senderOn(authz.AllDomainsAnchor())

	runDecisions(t, []decision{
		{"sends for one Domain", all, authz.Create, authz.Batches(example), true},
		{"sends for the other", all, authz.Create, authz.Batches(other), true},
		{"cannot read a Domain", all, authz.Read, authz.Domain(example), false},
		{"cannot create a Template", all, authz.Create, authz.Templates(example), false},
		{"cannot create a Domain", all, authz.Create, authz.Domains(), false},
	})
}

// SetTrackingPolicy is update on domains/<name>, which by domination also reaches that Domain's
// Templates. ADR 0008 accepts that the two are not separable: both are things a Domain
// administrator does, and a .../tracking path would name no entity in the language.
func TestSetTrackingPolicyIsUpdateOnTheDomain(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))

	runDecisions(t, []decision{
		{"updates its own Domain", owner, authz.Update, authz.Domain(example), true},
		{"reaches that Domain's Templates by domination", owner, authz.Update, authz.Template(example, "welcome"), true},
		{"reaches the Templates collection too", owner, authz.Update, authz.Templates(example), true},
		{"cannot update another Domain", owner, authz.Update, authz.Domain(other), false},
		{"cannot update another Domain's Template", owner, authz.Update, authz.Template(other, "welcome"), false},
	})
}

// CreateDomain is create on the domains collection, which only the root Anchor
// reaches. Nothing narrower does, and the two near misses are asserted
// explicitly: domains/* is every Domain, not the collection above them.
func TestCreateDomainIsReachableOnlyFromTheRoot(t *testing.T) {
	runDecisions(t, []decision{
		{"admin on the root creates a Domain", adminOn(authz.RootAnchor()), authz.Create, authz.Domains(), true},
		{"admin on the root lists Domains", adminOn(authz.RootAnchor()), authz.List, authz.Domains(), true},
		{"admin on every Domain cannot create one", adminOn(authz.AllDomainsAnchor()), authz.Create, authz.Domains(), false},
		{"admin on every Domain cannot list them", adminOn(authz.AllDomainsAnchor()), authz.List, authz.Domains(), false},
		{"admin on one Domain cannot create one", adminOn(authz.DomainAnchor(example)), authz.Create, authz.Domains(), false},
		{"sender on every Domain cannot create one", senderOn(authz.AllDomainsAnchor()), authz.Create, authz.Domains(), false},
	})
}

// admin on the root is the Role that can do everything on every Domain: one at()
// rule holding the five resource Actions, extended to every kind by domination.
func TestAdminOnTheRootCanDoEverythingEverywhere(t *testing.T) {
	root := adminOn(authz.RootAnchor())

	for _, r := range everyResource() {
		for _, a := range resourceActions {
			t.Run(a.String()+" on "+r.String(), func(t *testing.T) {
				assert.True(t, authz.Can(root, a, r))
			})
		}
	}
}

// Same Role, one Domain: that Domain's owner, and nothing of any other.
func TestAdminOnOneDomainIsThatDomainsOwner(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))

	for _, a := range resourceActions {
		for _, r := range []authz.Resource{
			authz.Domain(example),
			authz.Batches(example),
			authz.Templates(example),
			authz.Template(example, "welcome"),
			authz.APIKeys(example),
			authz.APIKey(example, "key-1"),
			authz.Stats(example),
			authz.AggregatedStats(example),
		} {
			t.Run("reaches "+a.String()+" on "+r.String(), func(t *testing.T) {
				assert.True(t, authz.Can(owner, a, r))
			})
		}

		for _, r := range []authz.Resource{
			authz.Domains(),
			authz.Domain(other),
			authz.Batches(other),
			authz.Templates(other),
			authz.Template(other, "welcome"),
			authz.APIKeys(other),
			authz.APIKey(other, "key-1"),
			authz.Stats(other),
			authz.AggregatedStats(other),
		} {
			t.Run("does not reach "+a.String()+" on "+r.String(), func(t *testing.T) {
				assert.False(t, authz.Can(owner, a, r))
			})
		}
	}
}

// The every-Domain Anchor reaches inside every Domain and stops below the
// collection: its wildcard stands for exactly one segment, so it cannot match the
// shorter path above it.
func TestAdminOnEveryDomainReachesEveryDomainButNotTheCollection(t *testing.T) {
	all := adminOn(authz.AllDomainsAnchor())

	runDecisions(t, []decision{
		{"reaches one Domain", all, authz.Update, authz.Domain(example), true},
		{"reaches the other", all, authz.Update, authz.Domain(other), true},
		{"reaches beneath a Domain", all, authz.Delete, authz.APIKey(other, "key-1"), true},
		{"reaches the counters", all, authz.Read, authz.AggregatedStats(example), true},
		{"does not reach the collection", all, authz.Read, authz.Domains(), false},
		{"does not reach the collection to list", all, authz.List, authz.Domains(), false},
	})
}

// Authority over the per-Delivery rows implies authority over the counters,
// because stats/aggregated is a child of stats rather than its sibling. The
// converse must not hold, and nothing in the seeded catalogue can express it.
func TestStatisticsNestRatherThanSitAsSiblings(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))

	runDecisions(t, []decision{
		{"reads the per-Delivery rows", owner, authz.Read, authz.Stats(example), true},
		{"reads the counters beneath them", owner, authz.Read, authz.AggregatedStats(example), true},
	})
}

func TestAPrincipalWithNoGrantsCanDoNothing(t *testing.T) {
	// Not the same as an unauthenticated request, which Guard reports
	// separately: this is a credential whose authority has been revoked.
	empty := principalWith()

	for _, r := range everyResource() {
		for _, a := range resourceActions {
			assert.False(t, authz.Can(empty, a, r))
		}
	}
}

// The zero value of a Grant — and so of its Anchor — must confer nothing. This is what the
// root's explicit flag protects: were "everything" an empty pattern, a forgotten assignment
// would be the widest authority in the system rather than the narrowest.
func TestAZeroValueGrantConfersNothing(t *testing.T) {
	p := principalWith(authz.Grant{})

	for _, r := range everyResource() {
		for _, a := range resourceActions {
			assert.False(t, authz.Can(p, a, r), "zero Grant reached %s on %s", a, r)
		}
	}

	// And the root, for contrast, covers every one of them.
	root := adminOn(authz.RootAnchor())
	for _, r := range everyResource() {
		assert.True(t, authz.Can(root, authz.Read, r))
	}
}

// A Resource that is not well formed — a zero domain name or a blank identifier reaching a
// constructor — is covered by nothing, not even the root, so programming errors fail closed
// rather than falling back on whatever the shorter path would have matched.
func TestAMalformedResourceIsCoveredByNothing(t *testing.T) {
	root := adminOn(authz.RootAnchor())

	runDecisions(t, []decision{
		{"zero Resource", root, authz.Read, authz.Resource{}, false},
		{"zero domain name", root, authz.Read, authz.Domain(values.DomainName{}), false},
		{"blank Template identifier", root, authz.Read, authz.Template(example, ""), false},
		{"zero domain name beneath a Domain", root, authz.Read, authz.Batches(values.DomainName{}), false},
	})
}

func TestCatalogueHoldsExactlyAdminAndSender(t *testing.T) {
	assert.Equal(t, []authz.RoleName{authz.RoleAdmin, authz.RoleSender}, authz.RoleNames())
}

// admin holds attribute and sender does not: the credential that administers Kannon is the one a
// front-end holds, so it is the one with people to name, while a customer's send key must not be
// able to claim a Batch was sent on somebody else's behalf (ADR 0009). And attribute is an Action
// like any other, so it reaches exactly as far as its Grant's Anchor and no further.
func TestAdminHoldsAttributeAndSenderDoesNot(t *testing.T) {
	runDecisions(t, []decision{
		{"admin on the root names who asked at the collection", adminOn(authz.RootAnchor()), authz.Attribute, authz.Domains(), true},
		{"admin on the root, inside a Domain", adminOn(authz.RootAnchor()), authz.Attribute, authz.Template(example, "welcome"), true},
		{"admin on one Domain, within it", adminOn(authz.DomainAnchor(example)), authz.Attribute, authz.APIKeys(example), true},
		{"admin on one Domain, not in another", adminOn(authz.DomainAnchor(example)), authz.Attribute, authz.APIKeys(other), false},
		{"admin on one Domain, not above it", adminOn(authz.DomainAnchor(example)), authz.Attribute, authz.Domains(), false},
		{"sender, on the Batches it may create", senderOn(authz.DomainAnchor(example)), authz.Attribute, authz.Batches(example), false},
		{"sender on every Domain, still not", senderOn(authz.AllDomainsAnchor()), authz.Attribute, authz.Batches(example), false},
	})
}

// The same rule as a sweep over the whole catalogue: nothing but admin reaches attribute anywhere
// in the tree. Written as an exclusion rather than a list of the Roles that must not hold it, so
// that a Role added to the catalogue is refused this power until somebody says otherwise here.
func TestNoRoleButAdminReachesAttribute(t *testing.T) {
	for _, g := range everyGrant(t) {
		if g.Role() == authz.RoleAdmin {
			continue
		}
		p := principalWith(g)
		for _, r := range everyResource() {
			assert.False(t, authz.Can(p, authz.Attribute, r), "%s reached attribute on %s", g, r)
		}
	}
}

// ADR 0008 records that every rule holding create also holds read, so a credential can read back
// what it just created. admin satisfies it; sender is the one enumerated exception, since giving
// it read would widen every API Key in circulation rather than tidy anything up.
func TestCreateImpliesReadExceptForSender(t *testing.T) {
	var exceptions []string

	for _, g := range everyGrant(t) {
		p := principalWith(g)
		for _, r := range everyResource() {
			if authz.Can(p, authz.Create, r) && !authz.Can(p, authz.Read, r) {
				exceptions = append(exceptions, g.String()+" -> "+r.String())
			}
		}
	}

	assert.ElementsMatch(t, []string{
		"sender on domains/* -> domains/example.com/batches",
		"sender on domains/* -> domains/other.com/batches",
		"sender on domains/example.com -> domains/example.com/batches",
		"sender on domains/other.com -> domains/other.com/batches",
	}, exceptions)
}

// Require is Can as an error, for call sites that propagate rather than branch.
func TestRequireReportsForbidden(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))

	require.NoError(t, authz.Require(owner, authz.Update, authz.Domain(example)))
	assert.ErrorIs(t, authz.Require(owner, authz.Update, authz.Domain(other)), authz.ErrForbidden)
}
