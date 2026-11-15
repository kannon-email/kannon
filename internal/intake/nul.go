package intake

import (
	"errors"
	"strings"
)

// errNUL refuses content Postgres text cannot store: left to the insert, it fails as an internal
// error rather than as the caller's mistake.
var errNUL = invalid(errors.New("subject, sender alias, HTML, template ID or fields contain a NUL byte"))

// containsNUL reports whether any text, global field or Recipient field holds a NUL byte.
func containsNUL[R any](texts []string, global map[string]string, recipients []R, fields func(R) map[string]string) bool {
	for _, text := range texts {
		if strings.ContainsRune(text, 0) {
			return true
		}
	}
	if fieldsContainNUL(global) {
		return true
	}
	for _, r := range recipients {
		if fieldsContainNUL(fields(r)) {
			return true
		}
	}
	return false
}

func fieldsContainNUL(fields map[string]string) bool {
	for k, v := range fields {
		if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return true
		}
	}
	return false
}
