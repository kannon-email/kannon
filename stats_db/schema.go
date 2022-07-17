package statsdbschema

import (
	_ "embed"
)

//go:embed schema.sql
var Schema string
