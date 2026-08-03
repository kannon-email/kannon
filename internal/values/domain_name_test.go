package values_test

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCanonicalises(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already canonical", "example.com", "example.com"},
		{"upper case", "EXAMPLE.COM", "example.com"},
		{"mixed case", "Example.Com", "example.com"},
		// Surrounding whitespace is trimmed rather than rejected: a trailing
		// newline out of a config file or an HTTP header is ordinary. Internal
		// whitespace is a different matter and is rejected below.
		{"surrounding space", "  example.com  ", "example.com"},
		{"trailing newline", "example.com\n", "example.com"},
		{"subdomain", "mail.example.co.uk", "mail.example.co.uk"},
		{"hyphen", "my-domain.example", "my-domain.example"},
		{"underscore", "_dmarc.example.com", "_dmarc.example.com"},
		{"digits", "123.example.com", "123.example.com"},
		// Two labels is the shortest shape a real mail domain takes, and the
		// dot requirement below must not disturb it.
		{"short two-label domain", "a.com", "a.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := values.Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"only space", "   "},
		{"path separator", "a.com/templates"},
		{"leading path separator", "/a.com"},
		{"wildcard", "*.example.com"},
		{"bare wildcard", "*"},
		{"at sign", "a.com@b.com"},
		{"leading dot", ".example.com"},
		{"trailing dot", "example.com."},
		{"empty label", "example..com"},
		{"space inside", "exa mple.com"},
		{"tab inside", "exa\tmple.com"},
		{"newline inside", "exam\nple.com"},
		{"null byte", "example.com\x00"},
		{"percent encoding", "a.com%2ftemplates"},
		{"colon", "example.com:443"},
		{"homoglyph", "examplе.com"}, // Cyrillic 'е'
		{"too long", strings.Repeat("a", 255)},
		// Single-label names are valid hostnames but also segments of the authorization
		// Resource tree (ADR 0008): a Domain named "templates" would turn "domains/templates"
		// into an alias for another node of the tree, so the dot removes that class.
		{"single label", "templates"},
		{"single label localhost", "localhost"},
		{"single label apikeys", "apikeys"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := values.Parse(tc.in)
			assert.Error(t, err)
		})
	}
}

// The two spellings must land on one Domain, since the whole authority model
// rests on a domain name denoting exactly one thing.
func TestCaseDifferingSpellingsAreOneDomain(t *testing.T) {
	lower, err := values.Parse("test.com")
	require.NoError(t, err)
	upper, err := values.Parse("TEST.com")
	require.NoError(t, err)

	assert.Equal(t, lower, upper)
	assert.Equal(t, lower.String(), upper.String())
}

func TestZeroValueNamesNoDomain(t *testing.T) {
	var n values.DomainName

	assert.True(t, n.IsZero())
	assert.Empty(t, n.String())

	parsed, err := values.Parse("example.com")
	require.NoError(t, err)
	assert.False(t, parsed.IsZero())
}

// Comparability is relied on for map keys and ==.
func TestUsableAsMapKey(t *testing.T) {
	seen := map[values.DomainName]int{}
	seen[values.MustParse("a.com")]++
	seen[values.MustParse("A.com")]++

	assert.Equal(t, map[values.DomainName]int{values.MustParse("a.com"): 2}, seen)
}

func TestMustParsePanicsOnInvalid(t *testing.T) {
	assert.Panics(t, func() { values.MustParse("a.com/x") })
	assert.NotPanics(t, func() { values.MustParse("a.com") })
}
