package authz_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attenuation is one row of the narrowing tables: given this Principal, what does asking for
// these Resources leave it holding, and what does it report dropping? wantGrants is rendered,
// since most narrowed Anchors are paths NewGrant refuses and there is no constructor for them.
type attenuation struct {
	name        string
	principal   authz.Principal
	request     []authz.Resource
	wantGrants  []string
	wantDropped []authz.Resource
}

func runAttenuations(t *testing.T, tests []attenuation) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			narrowed, dropped := tc.principal.Attenuate(tc.request...)

			var got []string
			for _, g := range narrowed.Grants() {
				got = append(got, g.String())
			}
			assert.Equal(t, tc.wantGrants, got, "%s asked for %v", tc.principal, tc.request)
			assertDropped(t, tc.wantDropped, dropped)

			// Asserted on every row rather than in a test of its own: narrowing changes what
			// may be done and never who did it, and an Attribution names who asked, which
			// reach cannot affect.
			assert.Equal(t, tc.principal.ID(), narrowed.ID())
			assert.Equal(t, tc.principal.Attribution(), narrowed.Attribution())
		})
	}
}

// assertDropped compares the dropped list by value and in order: rendering is display only, so
// two different Resources may render alike, and the order is meaningful — it is the order they
// were asked for.
func assertDropped(t *testing.T, want, got []authz.Resource) {
	t.Helper()
	require.Len(t, got, len(want), "dropped %v", got)
	for i := range want {
		assert.True(t, want[i].Equal(got[i]), "expected %s to be dropped, got %s", want[i], got[i])
	}
}

// What narrows is the Anchor; the Role travels unchanged, so narrowing where authority lands
// never changes what it is. A Role of pure shape narrows to any concrete path it covers, down
// to a single item — what lets a front-end scope one request to the object it concerns.
func TestAttenuateNarrowsTheAnchorAndLeavesTheRoleAlone(t *testing.T) {
	root := adminOn(authz.RootAnchor())
	all := adminOn(authz.AllDomainsAnchor())

	runAttenuations(t, []attenuation{
		{
			name:       "to one Domain",
			principal:  root,
			request:    []authz.Resource{authz.Domain(example)},
			wantGrants: []string{"admin on domains/example.com"},
		},
		{
			name:       "to a collection inside a Domain",
			principal:  root,
			request:    []authz.Resource{authz.Templates(example)},
			wantGrants: []string{"admin on domains/example.com/templates"},
		},
		{
			// The acceptance criterion a pure-shape Role owes the model: an
			// Anchor no Grant could have been issued on.
			name:       "to a single Template",
			principal:  root,
			request:    []authz.Resource{authz.Template(example, "welcome")},
			wantGrants: []string{"admin on domains/example.com/templates/welcome"},
		},
		{
			name:       "to one API Key",
			principal:  root,
			request:    []authz.Resource{authz.APIKey(example, "key-1")},
			wantGrants: []string{"admin on domains/example.com/apikeys/key-1"},
		},
		{
			name:       "to the counters, below the rows that carry an address",
			principal:  root,
			request:    []authz.Resource{authz.AggregatedStats(example)},
			wantGrants: []string{"admin on domains/example.com/stats/aggregated"},
		},
		{
			// NewGrant refuses this Anchor and narrowing admits it, which is the whole
			// difference between the two predicates. Not a widening: the root already reached
			// the collection, so this row can only ever shrink reach.
			name:       "to the domains collection, which is grantable to nobody",
			principal:  root,
			request:    []authz.Resource{authz.Domains()},
			wantGrants: []string{"admin on domains"},
		},
		{
			name:       "from every Domain to one of them",
			principal:  all,
			request:    []authz.Resource{authz.Domain(other)},
			wantGrants: []string{"admin on domains/other.com"},
		},
		{
			name:       "from every Domain to a path inside one",
			principal:  all,
			request:    []authz.Resource{authz.Batches(example)},
			wantGrants: []string{"admin on domains/example.com/batches"},
		},
		{
			name:       "to several paths at once",
			principal:  all,
			request:    []authz.Resource{authz.Domain(example), authz.Template(other, "welcome")},
			wantGrants: []string{"admin on domains/example.com", "admin on domains/other.com/templates/welcome"},
		},
	})
}

// A typed Role does not narrow beneath the Anchor kind its rules were written against: sender
// at domains/example.com/batches would compose .../batches/batches, a different and
// meaningless authority. The last two rows show admin accepting what sender refuses.
func TestATypedRoleNeverNarrowsBeneathItsDeclaredKind(t *testing.T) {
	all := senderOn(authz.AllDomainsAnchor())
	own := senderOn(authz.DomainAnchor(example))

	runAttenuations(t, []attenuation{
		{
			// The narrowing a domain-scoped Role does do: still a Domain.
			name:       "from every Domain to one Domain",
			principal:  all,
			request:    []authz.Resource{authz.Domain(example)},
			wantGrants: []string{"sender on domains/example.com"},
		},
		{
			name:        "refuses one Domain's Batches",
			principal:   all,
			request:     []authz.Resource{authz.Batches(example)},
			wantDropped: []authz.Resource{authz.Batches(example)},
		},
		{
			name:        "refuses the Batches of the Domain it is already anchored on",
			principal:   own,
			request:     []authz.Resource{authz.Batches(example)},
			wantDropped: []authz.Resource{authz.Batches(example)},
		},
		{
			name:        "refuses one Template",
			principal:   all,
			request:     []authz.Resource{authz.Template(example, "welcome")},
			wantDropped: []authz.Resource{authz.Template(example, "welcome")},
		},
		{
			name:        "refuses a Domain's statistics",
			principal:   all,
			request:     []authz.Resource{authz.Stats(example)},
			wantDropped: []authz.Resource{authz.Stats(example)},
		},
		{
			// Above its kind as well as below it: the collection is not a Domain,
			// and domains/* does not cover the shorter path anyway.
			name:        "refuses the domains collection",
			principal:   all,
			request:     []authz.Resource{authz.Domains()},
			wantDropped: []authz.Resource{authz.Domains()},
		},
		{
			name:       "a pure-shape Role accepts the same Batches path",
			principal:  adminOn(authz.AllDomainsAnchor()),
			request:    []authz.Resource{authz.Batches(example)},
			wantGrants: []string{"admin on domains/example.com/batches"},
		},
		{
			name:       "a pure-shape Role accepts the same Template",
			principal:  adminOn(authz.AllDomainsAnchor()),
			request:    []authz.Resource{authz.Template(example, "welcome")},
			wantGrants: []string{"admin on domains/example.com/templates/welcome"},
		},
	})
}

// Asking for more than one holds yields less rather than more. The result is an
// intersection, so there is no subset check to forget and no route by which a
// request for something unheld becomes authority over it.
func TestAttenuateCannotWiden(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))
	own := senderOn(authz.DomainAnchor(example))

	runAttenuations(t, []attenuation{
		{
			name:        "another Domain is not held",
			principal:   owner,
			request:     []authz.Resource{authz.Domain(other)},
			wantDropped: []authz.Resource{authz.Domain(other)},
		},
		{
			name:        "a path inside another Domain is not held",
			principal:   owner,
			request:     []authz.Resource{authz.Template(other, "welcome")},
			wantDropped: []authz.Resource{authz.Template(other, "welcome")},
		},
		{
			// Wider than held: the collection is above the Anchor, so the
			// request shrinks authority to nothing rather than growing it.
			name:        "the collection above the Anchor is not held",
			principal:   owner,
			request:     []authz.Resource{authz.Domains()},
			wantDropped: []authz.Resource{authz.Domains()},
		},
		{
			name:        "a Domain-scoped Role cannot reach another Domain's Batches",
			principal:   own,
			request:     []authz.Resource{authz.Batches(other)},
			wantDropped: []authz.Resource{authz.Batches(other)},
		},
		{
			name:        "a Principal holding nothing narrows to nothing",
			principal:   principalWith(),
			request:     []authz.Resource{authz.Domain(example)},
			wantDropped: []authz.Resource{authz.Domain(example)},
		},
		{
			// The zero Grant covers nothing and names a Role the catalogue does
			// not hold. Both make it confer nothing, and narrowing something
			// that confers nothing must not conjure something that does.
			name:        "a zero-value Grant narrows to nothing",
			principal:   principalWith(authz.Grant{}),
			request:     []authz.Resource{authz.Domain(example)},
			wantDropped: []authz.Resource{authz.Domain(example)},
		},
		{
			// A blank identifier reaching a constructor is a programming error.
			// Nothing covers a malformed Resource, not even the root, so it is
			// dropped rather than narrowed to a path that would cover nothing.
			name:        "a malformed Resource is covered by nothing",
			principal:   adminOn(authz.RootAnchor()),
			request:     []authz.Resource{authz.Template(example, "")},
			wantDropped: []authz.Resource{authz.Template(example, "")},
		},
	})

	// And the authority is genuinely absent afterwards, not merely absent from
	// the rendered Grants.
	narrowed, _ := owner.Attenuate(authz.Domain(other), authz.Domains())
	runDecisions(t, []decision{
		{"cannot reach the Domain it asked for", narrowed, authz.Read, authz.Domain(other), false},
		{"cannot reach the collection it asked for", narrowed, authz.List, authz.Domains(), false},
		{"cannot reach what it did hold either, having asked for neither", narrowed, authz.Read, authz.Domain(example), false},
	})

	// The Principal it was narrowed from is untouched: Attenuate returns a value
	// and never edits the one it was called on.
	assert.True(t, authz.Can(owner, authz.Read, authz.Domain(example)))
}

// One uncovered path does not fail the whole call: the covered ones still narrow, and the caller
// learns which path it asked for in vain — silent narrowing is safe but hides a typo that would
// otherwise refuse a user with no way to learn why.
func TestAttenuateDropsOnlyThePathsItCannotNarrowTo(t *testing.T) {
	all := adminOn(authz.AllDomainsAnchor())

	runAttenuations(t, []attenuation{
		{
			name:      "one uncovered path among covered ones",
			principal: all,
			request: []authz.Resource{
				authz.Domain(example),
				authz.Domains(),
				authz.Domain(other),
			},
			wantGrants:  []string{"admin on domains/example.com", "admin on domains/other.com"},
			wantDropped: []authz.Resource{authz.Domains()},
		},
		{
			name:      "a typed Role's refusal sits beside its successful narrowing",
			principal: senderOn(authz.AllDomainsAnchor()),
			request: []authz.Resource{
				authz.Batches(example),
				authz.Domain(other),
				authz.Template(other, "welcome"),
			},
			wantGrants: []string{"sender on domains/other.com"},
			wantDropped: []authz.Resource{
				authz.Batches(example),
				authz.Template(other, "welcome"),
			},
		},
		{
			name: "several Roles, one of which refuses the request",
			principal: principalWith(
				authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()),
				authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)),
			),
			request:    []authz.Resource{authz.Batches(example)},
			wantGrants: []string{"admin on domains/example.com/batches"},
		},
	})
}

// Two Grants of the same Role in one place are one Grant; two of different Roles are not. A
// duplicate adds nothing to the union that is a Principal's authority while adding a second
// identical line to every record that renders it.
func TestAttenuateCollapsesRepetitionButNotDistinctRoles(t *testing.T) {
	runAttenuations(t, []attenuation{
		{
			// Asking for nothing gets nothing. A caller that computed an empty
			// list of paths and expected undiminished authority back has a bug,
			// and it fails closed.
			name:      "no Resources at all",
			principal: adminOn(authz.RootAnchor()),
		},
		{
			name:       "the same Resource twice",
			principal:  adminOn(authz.RootAnchor()),
			request:    []authz.Resource{authz.Domain(example), authz.Domain(example)},
			wantGrants: []string{"admin on domains/example.com"},
		},
		{
			name:        "the same uncovered Resource twice is reported once",
			principal:   adminOn(authz.DomainAnchor(example)),
			request:     []authz.Resource{authz.Domain(other), authz.Domain(other)},
			wantDropped: []authz.Resource{authz.Domain(other)},
		},
		{
			name: "one path covered by two Grants of the same Role",
			principal: principalWith(
				authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()),
				authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()),
				authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(example)),
			),
			request:    []authz.Resource{authz.Domain(example)},
			wantGrants: []string{"admin on domains/example.com"},
		},
		{
			// Kept apart: their union is precisely what the Principal held there,
			// and collapsing them would silently drop one Role's Actions.
			name: "one path covered by two different Roles",
			principal: principalWith(
				authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()),
				authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)),
			),
			request:    []authz.Resource{authz.Domain(example)},
			wantGrants: []string{"admin on domains/example.com", "sender on domains/example.com"},
		},
	})

	// Both Roles still confer what they did, in the one place that is left.
	both := principalWith(
		authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()),
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)),
	)
	narrowed, dropped := both.Attenuate(authz.Domain(example))
	require.Empty(t, dropped)

	runDecisions(t, []decision{
		{"administers the Domain it narrowed to", narrowed, authz.Update, authz.Domain(example), true},
		{"still sends for it", narrowed, authz.Create, authz.Batches(example), true},
		{"neither Role reaches the other Domain", narrowed, authz.Read, authz.Domain(other), false},
	})
}

// Prefix domination is what makes concrete paths sufficient: a narrowed Anchor
// still reaches everything beneath it without naming any of it, and stops dead
// above it.
func TestANarrowedAnchorStillDominatesWhatLiesBeneathIt(t *testing.T) {
	root := adminOn(authz.RootAnchor())

	toDomain, _ := root.Attenuate(authz.Domain(example))
	toTemplates, _ := root.Attenuate(authz.Templates(example))
	toTemplate, _ := root.Attenuate(authz.Template(example, "welcome"))
	toStats, _ := root.Attenuate(authz.Stats(example))

	runDecisions(t, []decision{
		{"a Domain owns the Domain", toDomain, authz.Update, authz.Domain(example), true},
		{"a Domain reaches its Templates unnamed", toDomain, authz.Update, authz.Template(example, "welcome"), true},
		{"a Domain reaches its counters unnamed", toDomain, authz.Read, authz.AggregatedStats(example), true},
		{"a Domain reaches no other Domain", toDomain, authz.Read, authz.Domain(other), false},
		{"a Domain no longer creates one", toDomain, authz.Create, authz.Domains(), false},

		{"a collection reaches the items in it", toTemplates, authz.Update, authz.Template(example, "welcome"), true},
		{"a collection does not reach the Domain above it", toTemplates, authz.Update, authz.Domain(example), false},
		{"a collection does not reach a sibling collection", toTemplates, authz.Read, authz.APIKeys(example), false},

		{"a single item is reachable", toTemplate, authz.Update, authz.Template(example, "welcome"), true},
		{"a single item is not its neighbour", toTemplate, authz.Update, authz.Template(example, "receipt"), false},
		{"a single item is not the collection above it", toTemplate, authz.List, authz.Templates(example), false},
		{"a single item is not the same item of another Domain", toTemplate, authz.Update, authz.Template(other, "welcome"), false},

		// The one place in the tree where something real lies beneath a single
		// node: the counters are a child of the per-Delivery rows, so authority
		// over the rows implies authority over the counters.
		{"the statistics reach the counters beneath them", toStats, authz.Read, authz.AggregatedStats(example), true},
		{"the statistics do not reach the Domain above them", toStats, authz.Read, authz.Domain(example), false},
	})
}

// A domain-scoped Role narrows to one Domain and still does there exactly what
// it did before: the Anchor moved, the rules did not.
func TestADomainScopedRoleStillSendsForTheDomainItNarrowedTo(t *testing.T) {
	all := senderOn(authz.AllDomainsAnchor())

	narrowed, dropped := all.Attenuate(authz.Domain(example))
	require.Empty(t, dropped)

	runDecisions(t, []decision{
		{"sends for the Domain it narrowed to", narrowed, authz.Create, authz.Batches(example), true},
		{"no longer sends for any other", narrowed, authz.Create, authz.Batches(other), false},
		{"gained nothing on the way", narrowed, authz.Read, authz.Domain(example), false},
		{"still cannot rewrite a Template", narrowed, authz.Update, authz.Template(example, "welcome"), false},
		{"still cannot mint an API Key", narrowed, authz.Create, authz.APIKeys(example), false},
	})

	// The one it held before is unchanged.
	assert.True(t, authz.Can(all, authz.Create, authz.Batches(other)))
}

// Narrowing changes what may be done and never who did it, and says nothing about who asked.
// Both are asserted on every row above; this table is the one that carries an Attribution
// through, including through a narrowing that yields nothing at all.
func TestAttenuatePreservesIdentityAndCarriesAnyAttributionThrough(t *testing.T) {
	claiming := adminOn(authz.RootAnchor()).WithAttribution("alice@corp.com")
	plain := adminOn(authz.DomainAnchor(example))

	runAttenuations(t, []attenuation{
		{
			name:       "an Attribution survives a narrowing that succeeds",
			principal:  claiming,
			request:    []authz.Resource{authz.Template(example, "welcome")},
			wantGrants: []string{"admin on domains/example.com/templates/welcome"},
		},
		{
			name:        "an Attribution survives a narrowing that drops everything",
			principal:   claiming.WithAttribution("bob@corp.com"),
			request:     []authz.Resource{authz.Template(example, "")},
			wantDropped: []authz.Resource{authz.Template(example, "")},
		},
		{
			name:      "an Attribution survives a narrowing to nothing at all",
			principal: claiming,
		},
		{
			name:       "a Principal claiming nothing keeps claiming nothing",
			principal:  plain,
			request:    []authz.Resource{authz.Domain(example)},
			wantGrants: []string{"admin on domains/example.com"},
		},
	})

	// Spelled out once beyond the table, since these are the two properties the
	// mechanism is named for: the identity is the credential's, and the claim is
	// carried rather than consulted.
	narrowed, _ := claiming.Attenuate(authz.Domain(example))
	assert.Equal(t, "key-1@example.com", narrowed.ID())
	assert.Equal(t, authz.Attribution("alice@corp.com"), narrowed.Attribution())
}

// Re-attenuating keeps shrinking: idempotent at worst, never regrowing. There is
// no route by which a narrowed Principal recovers reach it gave up, because each
// step is again an intersection with what the step before it left.
func TestReAttenuationKeepsShrinking(t *testing.T) {
	root := adminOn(authz.RootAnchor())

	toDomain, dropped := root.Attenuate(authz.Domain(example))
	require.Empty(t, dropped)

	toTemplates, dropped := toDomain.Attenuate(authz.Templates(example))
	require.Empty(t, dropped)

	toTemplate, dropped := toTemplates.Attenuate(authz.Template(example, "welcome"))
	require.Empty(t, dropped)

	// Idempotent: asking again for what it already is changes nothing.
	again, dropped := toTemplate.Attenuate(authz.Template(example, "welcome"))
	require.Empty(t, dropped)
	assert.Equal(t, toTemplate.String(), again.String())

	// Every attempt to climb back up is dropped, at every level.
	regrow, dropped := toTemplate.Attenuate(
		authz.Templates(example),
		authz.Domain(example),
		authz.Domains(),
		authz.Domain(other),
	)
	assertDropped(t, []authz.Resource{
		authz.Templates(example),
		authz.Domain(example),
		authz.Domains(),
		authz.Domain(other),
	}, dropped)
	assert.Empty(t, regrow.Grants())

	runDecisions(t, []decision{
		{"step one reaches inside the Domain", toDomain, authz.Update, authz.Template(example, "receipt"), true},
		{"step two lost the Domain itself", toTemplates, authz.Update, authz.Domain(example), false},
		{"step two still reaches every Template", toTemplates, authz.Update, authz.Template(example, "receipt"), true},
		{"step three lost every other Template", toTemplate, authz.Update, authz.Template(example, "receipt"), false},
		{"step three keeps the one it named", toTemplate, authz.Update, authz.Template(example, "welcome"), true},
		{"asking to climb back yields nothing", regrow, authz.Read, authz.Domain(example), false},
		{"and nothing where it stood either", regrow, authz.Read, authz.Template(example, "welcome"), false},
	})

	// The same shape for a typed Role, whose steps are shorter: it can only ever
	// stand on a Domain, so re-attenuation either holds still or empties it.
	sender := senderOn(authz.AllDomainsAnchor())
	one, dropped := sender.Attenuate(authz.Domain(example))
	require.Empty(t, dropped)
	same, dropped := one.Attenuate(authz.Domain(example))
	require.Empty(t, dropped)
	assert.Equal(t, one.String(), same.String())

	none, dropped := one.Attenuate(authz.Domain(other))
	assertDropped(t, []authz.Resource{authz.Domain(other)}, dropped)
	assert.Empty(t, none.Grants())
}

// A Resource segment that is literally an asterisk never matches as a wildcard. That is why a
// wildcard is a flag rather than the token: were the Resource-to-Anchor conversion textual, an
// identifier of "*" would widen authority here instead of narrowing it.
func TestALiteralAsteriskSegmentNeverMatchesAsAWildcard(t *testing.T) {
	owner := adminOn(authz.DomainAnchor(example))

	narrowed, dropped := owner.Attenuate(authz.Template(example, "*"))
	require.Empty(t, dropped)
	require.Len(t, narrowed.Grants(), 1)

	runDecisions(t, []decision{
		{"reaches the Template actually named", narrowed, authz.Read, authz.Template(example, "*"), true},
		{"does not reach another Template", narrowed, authz.Read, authz.Template(example, "welcome"), false},
		{"does not reach the collection above it", narrowed, authz.Read, authz.Templates(example), false},
	})
}
