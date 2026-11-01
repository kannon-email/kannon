package utils_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allocated reports the bytes f allocates, so a test can tell a measure from a render.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// unrendered bounds what measuring or refusing an amplified body may allocate: far below the five
// gigabytes rendering it would, and above what the race detector adds on top, since under -race
// sync.Pool drops a quarter of what is put back and regexp allocates a fresh machine per match.
const unrendered = 512 << 20

// amplified is a body of well under a megabyte that renders to five gigabytes:
// a hundred thousand placeholders against a fifty-kilobyte value.
func amplified() (string, map[string]string) {
	return strings.Repeat("{{a}}", 100_000), map[string]string{"a": strings.Repeat("x", 50_000)}
}

func TestLiteralFieldsSizeIsTheRenderedLength(t *testing.T) {
	for _, tc := range []struct {
		text   string
		fields map[string]string
	}{
		{"", nil},
		{"no placeholders", map[string]string{"a": "x"}},
		{"Hi {{ name }}, {{name}}!", map[string]string{"name": "Alice"}},
		{"{{ unknown }} stays", map[string]string{"name": "Alice"}},
		{"{{a}}{{b}}", map[string]string{"a": "{{b}}", "b": "long value"}},
		{"{{a}}", map[string]string{"a": ""}},
		{"{{ a b }} {{}}", map[string]string{"a b": "spaced", "": "empty"}},
		{"cost: {{price}}", map[string]string{"price": "$5 ${0}"}},
	} {
		assert.Equal(t, len(utils.ReplaceLiteralFields(tc.text, tc.fields)), utils.LiteralFieldsSize(tc.text, tc.fields), tc.text)
	}
}

func TestLiteralFieldsSizeDoesNotRender(t *testing.T) {
	html, fields := amplified()
	var size int
	bytes := allocated(func() { size = utils.LiteralFieldsSize(html, fields) })
	require.Equal(t, 100_000*50_000, size)
	require.Less(t, bytes, uint64(unrendered), "measuring must not build the rendered string")
}

func TestReplaceCustomFieldsWithinRendersLikeReplaceCustomFields(t *testing.T) {
	for _, tc := range []struct {
		text   string
		fields map[string]string
	}{
		{"Hello {{name}} and {{ name }}", map[string]string{"name": "world"}},
		{"no placeholders", map[string]string{"name": "world"}},
		{"{{ unknown }}", map[string]string{"name": "world"}},
		// A value is a regexp template: these are what makes the legacy semantics
		// impossible to measure by length alone.
		{"cost: {{price}}", map[string]string{"price": "$5.00"}},
		{"cost: {{price}}", map[string]string{"price": "$$5"}},
		{"echo: {{ a }}", map[string]string{"a": "[$0][${0}]"}},
		{"group: {{(a)}}", map[string]string{"(a)": "<$1>"}},
		{"bad key: {{(}}", map[string]string{"(": "x"}},
	} {
		got, ok := utils.ReplaceCustomFieldsWithin(tc.text, tc.fields, 1<<20)
		require.True(t, ok, tc.text)
		assert.Equal(t, utils.ReplaceCustomFields(tc.text, tc.fields), got, tc.text)
	}
}

func TestReplaceCustomFieldsWithinRefusesPastTheLimit(t *testing.T) {
	got, ok := utils.ReplaceCustomFieldsWithin("Hi {{name}}", map[string]string{"name": "Alice"}, 8)
	require.True(t, ok)
	require.Equal(t, "Hi Alice", got)

	_, ok = utils.ReplaceCustomFieldsWithin("Hi {{name}}", map[string]string{"name": "Alice"}, 7)
	require.False(t, ok)

	// Text already past the limit is refused whether or not anything is substituted.
	_, ok = utils.ReplaceCustomFieldsWithin("too long", nil, 7)
	require.False(t, ok)
}

func TestReplaceCustomFieldsWithinDoesNotRenderAnAmplification(t *testing.T) {
	html, fields := amplified()
	var ok bool
	bytes := allocated(func() { _, ok = utils.ReplaceCustomFieldsWithin(html, fields, 500<<10) })
	require.False(t, ok)
	require.Less(t, bytes, uint64(unrendered), "refusing must not build the rendered string")
}

// `$0` expands to the whole placeholder, so a value of references multiplies
// the placeholder's own length, padded with spaces as far as the body allows.
func TestReplaceCustomFieldsWithinBoundsExpansion(t *testing.T) {
	html := strings.Repeat("{{a"+strings.Repeat(" ", 1000)+"}}", 100)
	fields := map[string]string{"a": strings.Repeat("$0", 10_000)}
	var ok bool
	bytes := allocated(func() { _, ok = utils.ReplaceCustomFieldsWithin(html, fields, 500<<10) })
	require.False(t, ok)
	require.Less(t, bytes, uint64(16<<20))
}

// A value may hold a placeholder a later key expands, in map order. Whichever
// order is taken, nothing longer than the limit comes back.
func TestReplaceCustomFieldsWithinBoundsNesting(t *testing.T) {
	fields := map[string]string{"a": strings.Repeat("{{b}}", 1000), "b": strings.Repeat("x", 50_000)}
	for range 20 {
		got, ok := utils.ReplaceCustomFieldsWithin("{{a}}", fields, 500<<10)
		if ok {
			require.Equal(t, strings.Repeat("{{b}}", 1000), got, "only b first leaves the nested placeholders unexpanded")
		}
	}
}

func TestReplaceCustomFieldsInURLWithinEscapesAndBounds(t *testing.T) {
	got, ok := utils.ReplaceCustomFieldsInURLWithin("https://x.example/u?e={{ email }}", map[string]string{"email": "a+b@x.example"}, 1<<10)
	require.True(t, ok)
	require.Equal(t, "https://x.example/u?e=a%2Bb%40x.example", got)

	html, fields := amplified()
	_, ok = utils.ReplaceCustomFieldsInURLWithin(html, fields, 500<<10)
	require.False(t, ok)
}
