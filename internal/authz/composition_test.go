// This file tests the two rule-composition shapes the seeded catalogue cannot
// reach, and the scope predicates that no seeded Role exercises.
//
// ADR 0008 admits both shapes — on(domains, ...) composes only at the root, and
// a multi-segment suffix is what makes metrics-reader's "counters but not the
// per-Delivery rows" expressible — but the catalogue seeds only admin and
// sender, so neither is reachable through Can from outside the package. The
// alternatives were to seed a Role nothing can issue, which ADR 0008 rejects as
// dead vocabulary that would churn when the grants table is designed, or to
// mutate the catalogue under a test, which makes the decision procedure depend
// on test ordering. Composing the rule here instead keeps both out.
//
// This is the one test file inside the package, and it stays honest about the
// seam: what it asserts is still *reach* — does this composition cover this
// Resource? — and never how a pattern is represented. The composition is a
// security-relevant path that will become reachable the moment the wider
// vocabulary is seeded, which is exactly when a latent bug in it would become a
// privilege escalation rather than an unused branch.
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

	// The domination that makes such a Role dangerous rather than merely broad.
	// ADR 0008 leaves provisioner out precisely because create composed at
	// domains dominates every create beneath it — the Batches and API Keys of
	// every Domain, including Domains that do not exist yet. Asserted so that
	// whoever proposes such a Role meets the consequence here first.
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

	// The load-bearing refusal: a Pattern longer than the Resource covers
	// nothing, so authority over the counters does not climb to the rows above
	// them. That asymmetry is what makes a metrics credential with zero
	// personal-data reach expressible at all — stats carries a Recipient address
	// and, under Full, an IP and user agent, while the counters carry none.
	assert.False(t, reach.covers(Stats(composed)),
		"the per-Delivery rows are above the suffix and must stay out of reach")
	assert.False(t, reach.covers(Domain(composed)),
		"nor does it climb to the Domain itself")
	assert.False(t, reach.covers(Templates(composed)),
		"nor sideways to another kind")
	assert.False(t, reach.covers(AggregatedStats(values.MustParse("other.com"))),
		"nor to another Domain's counters")
}

// TestEveryRuleHoldingCreateAlsoHoldsRead states ADR 0008's property at the
// level the ADR states it: the rule.
//
// The boundary test asserts the same thing through decisions, which is the right
// place for it but is a weaker statement — a Role holding create in one rule and
// read only via a broader at() rule would satisfy every decision while breaking
// the rule-level property the ADR describes ("Every rule holding create also
// holds read, visibly, so a credential can always read back what it just
// created"). Asserted here so that the two cannot drift apart.
//
// sender is the one sanctioned exception, and it is named rather than tolerated
// by a loosened predicate. ADR 0008 states the property of the vocabulary it
// documents and pins an API Key to "sender anchored on its own Domain … and
// nothing else"; giving sender read would widen every key in circulation. Any
// *second* exception has to be added here deliberately.
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

// TestTheScopePredicatesFailClosed holds the two guard rails no seeded Role
// exercises: the undeclared zero value, and the root scope.
//
// Both are asserted as verdicts rather than as representation. The zero value is
// the one that matters most: a catalogue entry whose author forgot to declare a
// scope must refuse every Grant and every narrowing, because concatenating a
// suffix onto the wrong node yields a different *meaning* rather than an error.
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

	// The one case where the two questions diverge, stated here so that a change
	// collapsing them back into one predicate breaks a test naming the reason:
	// grantability constrains what an operator may write down, while narrowing
	// only reduces authority that already exists.
	assert.False(t, scopeAny.accepts(kindOther),
		"a Grant is not issuable on a path inside a Domain")
	assert.True(t, scopeAny.admitsNarrowingTo(kindOther),
		"but a pure-shape Role narrows to one, down to a single Template")
}
