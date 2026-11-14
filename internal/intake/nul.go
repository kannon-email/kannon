package intake

import (
	"errors"
	"strings"
)

// errNUL refuses content Postgres text cannot store: left to the insert, it fails as an internal
// error rather than as the caller's mistake.
var errNUL = invalid(errors.New("subject, sender alias, HTML or fields contain a NUL byte"))

func containsNUL(texts []string, fieldSets ...map[string]string) bool {
	for _, text := range texts {
		if strings.ContainsRune(text, 0) {
			return true
		}
	}
	for _, fields := range fieldSets {
		for k, v := range fields {
			if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
				return true
			}
		}
	}
	return false
}
