package dbschema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSchemaCarriesNoPsqlMetaCommands guards the whole integration suite. Every
// package that provisions Postgres executes Schema over pgx, so a `\restrict`
// line surviving into it does not fail one test — it fails every suite in the
// repo at once, with `syntax error at or near "\"`, and the cause is nowhere
// near the tests that break.
func TestSchemaCarriesNoPsqlMetaCommands(t *testing.T) {
	for i, line := range strings.Split(Schema, "\n") {
		assert.False(t, strings.HasPrefix(strings.TrimSpace(line), `\`),
			"line %d is a psql meta-command and cannot be executed over a SQL connection: %q", i+1, line)
	}

	// And the strip took nothing else with it.
	assert.Contains(t, Schema, "CREATE TABLE public.sending_pool_emails")
	assert.Contains(t, Schema, "claimed_at timestamp without time zone")
}
