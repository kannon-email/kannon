package dbschema

import (
	_ "embed"

	"github.com/amacneil/dbmate/v2/pkg/dbutil"
)

//go:embed schema.sql
var rawSchema string

// Schema is db/schema.sql, ready to execute over a plain SQL connection.
//
// The file is written verbatim by `dbmate dump`, and pg_dump 15.14+/16.10+/17.6+
// wraps its output in `\restrict` / `\unrestrict` psql meta-commands (the
// CVE-2025-8714 mitigation). dbmate keeps them in the dump deliberately — with a
// fixed key, so the file stays byte-stable across dumps — and strips them again
// inside `dbmate load`. Kannon never loads the file through dbmate: the
// integration suites provision Postgres by executing this string over pgx
// (internal/tests.TestPostgresInit), which speaks SQL and not psql, and a
// meta-command reaching it fails the whole suite with `syntax error at or near
// "\"`. So the same strip dbmate applies on load is applied here, through
// dbmate's own implementation rather than a second copy of it.
var Schema = executableSchema(rawSchema)

func executableSchema(raw string) string {
	stripped, err := dbutil.StripPsqlMetaCommands([]byte(raw))
	if err != nil {
		// Only reachable if scanning the embedded dump fails. Hand back the dump
		// as it stands rather than panicking in package init: this package is
		// imported by the binary for Migrate, which does not touch Schema, and
		// whoever executes it will fail loudly on the meta-command itself.
		return raw
	}
	return string(stripped)
}
