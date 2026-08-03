package sqlc

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// foreignKeyViolation is the SQLSTATE Postgres raises for a statement a foreign
// key refuses: an insert naming a parent row that is not there, or a delete of a
// parent row a child still references.
const foreignKeyViolation = "23503"

// isForeignKeyViolation reports whether err is Postgres refusing a statement on
// a foreign key.
//
// Which key, and therefore what the refusal means, is left to the caller: a
// repository method touches one relationship, so the constraint that can fire is
// never in doubt at the call site, and pattern-matching constraint names here
// would put the schema's naming inside the error plumbing.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}
