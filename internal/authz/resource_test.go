package authz_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
)

// The shape of the tree, written down once. Authority over any of these paths
// reaches everything below it, so the nesting is the whole of what a Grant means
// and is worth pinning.
func TestResourcePaths(t *testing.T) {
	tests := []struct {
		name     string
		resource authz.Resource
		want     string
	}{
		{"the Domains collection", authz.Domains(), "domains"},
		{"one Domain", authz.Domain(example), "domains/example.com"},
		{"a Domain's Batches", authz.Batches(example), "domains/example.com/batches"},
		{"a Domain's Templates", authz.Templates(example), "domains/example.com/templates"},
		{"one Template", authz.Template(example, "welcome"), "domains/example.com/templates/welcome"},
		{"a Domain's API Keys", authz.APIKeys(other), "domains/other.com/apikeys"},
		{"one API Key", authz.APIKey(other, "key-1"), "domains/other.com/apikeys/key-1"},
		{"per-Delivery statistics", authz.Stats(example), "domains/example.com/stats"},
		// Beneath stats rather than beside it: authority over the per-Delivery
		// rows implies authority over the counters, which is true anyway since
		// anyone who can read every event can count them.
		{"the counters", authz.AggregatedStats(example), "domains/example.com/stats/aggregated"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.resource.String())
		})
	}
}

func TestResourceEqualityIsPerSegment(t *testing.T) {
	assert.True(t, authz.Domain(example).Equal(authz.Domain(values.MustParse("EXAMPLE.com"))))
	assert.False(t, authz.Domain(example).Equal(authz.Domain(other)))
	assert.False(t, authz.Domain(example).Equal(authz.Domains()))
	assert.False(t, authz.Templates(example).Equal(authz.Template(example, "welcome")))
}

// The Anchor renders for a human — in a log line, and in the error NewGrant
// returns when an Anchor is not grantable. The root is "*", and the zero Anchor is
// empty because it names nothing.
func TestAnchorRendersForDisplay(t *testing.T) {
	tests := []struct {
		name   string
		anchor authz.Anchor
		want   string
	}{
		{"the root", authz.RootAnchor(), "*"},
		{"every Domain", authz.AllDomainsAnchor(), "domains/*"},
		{"one Domain", authz.DomainAnchor(example), "domains/example.com"},
		{"the Anchor of a Resource", authz.AnchorOf(authz.Template(other, "welcome")), "domains/other.com/templates/welcome"},
		{"the zero Anchor", authz.Anchor{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.anchor.String())
		})
	}
}
