// This file tests the two rule-composition shapes the seeded catalogue cannot reach, and the
// scope predicates no seeded Role exercises. Composed here rather than by seeding dead
// vocabulary or mutating the catalogue under a test; it asserts reach, never representation.
package authz

import (
	"testing"

	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
)

// composed is a multi-label Domain, as internal/values requires.
var composed = values.MustParse("example.com")

// TestARootScopedSuffixComposesAtTheCollectionAndDominatesEverythingBeneathIt
// covers the root-anchor-plus-suffix composition, and pins the reason ADR 0008
// gives for leaving a provisioner Role out of the vocabulary.
func TestARootScopedSuffixComposesAtTheCollectionAndDominatesEverythingBeneathIt(t *testing.T) {
	// What on(domains, ...) reaches when anchored at the root.
	reach := RootAnchor().extend(childKind{segDomains})

	assert.True(t, reach.covers(Domains()),
		"a root-scoped suffix must reach the collection it names")

	// The domination that makes such a Role dangerous rather than merely broad: create composed
	// at domains dominates every create beneath it, for Domains that do not exist yet. Asserted
	// so that whoever proposes a provisioner Role meets the consequence here first.
	assert.True(t, reach.covers(Domain(composed)),
		"prefix domination reaches every Domain beneath the collection")
	assert.True(t, reach.covers(Batches(composed)),
		"and every Domain's Batches, which is why a provisioner Role needs its own design")
	assert.True(t, reach.covers(APIKey(composed, "key-1")),
		"and every Domain's API Keys")
}

// TestAMultiSegmentSuffixReachesTheCountersWithoutThePerDeliveryRows covers the
// multi-segment composition, which is the one the personal-data line depends on.
func TestAMultiSegmentSuffixReachesTheCountersWithoutThePerDeliveryRows(t *testing.T) {
	// What on(stats/aggregated, ...) reaches when anchored on one Domain.
	reach := DomainAnchor(composed).extend(childKind{segStats, segAggregated})

	assert.True(t, reach.covers(AggregatedStats(composed)),
		"the counters are what the suffix names")

	// The load-bearing refusal: a pattern longer than the Resource covers nothing, so authority
	// over the counters does not climb to the rows above them — which is what makes a metrics
	// credential with zero personal-data reach expressible at all.
	assert.False(t, reach.covers(Stats(composed)),
		"the per-Delivery rows are above the suffix and must stay out of reach")
	assert.False(t, reach.covers(Domain(composed)),
		"nor does it climb to the Domain itself")
	assert.False(t, reach.covers(Templates(composed)),
		"nor sideways to another kind")
	assert.False(t, reach.covers(AggregatedStats(values.MustParse("other.com"))),
		"nor to another Domain's counters")
}

// TestEveryRuleHoldingCreateAlsoHoldsRead states ADR 0008's property at the level the ADR states
// it: the rule, since a Role holding create in one rule and read via a broader at() rule would
// satisfy every decision. sender is the one named exception, not a loosened predicate.
func TestEveryRuleHoldingCreateAlsoHoldsRead(t *testing.T) {
	exceptions := map[RoleName]bool{RoleSender: true}

	for name, def := range catalogue {
		for i, r := range def.rules {
			if !r.holds(Create) {
				continue
			}
			if exceptions[name] {
				assert.False(t, r.holds(Read),
					"role %q is listed as an exception but now holds read: remove it from the list", name)
				continue
			}
			assert.True(t, r.holds(Read),
				"rule %d of role %q holds create without read, so a credential could not read back what it created",
				i, name)
		}
	}
}

// TestTheScopePredicatesFailClosed holds the two guard rails no seeded Role exercises: the
// undeclared zero value and the root scope. The zero value matters most — a forgotten scope must
// refuse every Grant, since a suffix on the wrong node yields a different meaning, not an error.
func TestTheScopePredicatesFailClosed(t *testing.T) {
	everyKind := []anchorKind{kindOther, kindRoot, kindDomain}

	for _, k := range everyKind {
		assert.False(t, scopeUndeclared.accepts(k),
			"an undeclared scope must be grantable nowhere")
		assert.False(t, scopeUndeclared.admitsNarrowingTo(k),
			"an undeclared scope must narrow nowhere")
	}

	// A root-scoped Role composes only at the root, on either question.
	assert.True(t, scopeRoot.accepts(kindRoot))
	assert.False(t, scopeRoot.accepts(kindDomain))
	assert.False(t, scopeRoot.accepts(kindOther))
	assert.True(t, scopeRoot.admitsNarrowingTo(kindRoot))
	assert.False(t, scopeRoot.admitsNarrowingTo(kindDomain))

	// The one case where the two questions diverge, stated so that a change collapsing them into
	// one predicate breaks a test naming the reason: grantability constrains what may be written
	// down, while narrowing only reduces authority that already exists.
	assert.False(t, scopeAny.accepts(kindOther),
		"a Grant is not issuable on a path inside a Domain")
	assert.True(t, scopeAny.admitsNarrowingTo(kindOther),
		"but a pure-shape Role narrows to one, down to a single Template")
}
