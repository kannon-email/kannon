package apikeys

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/nrednav/cuid2"
)

const IDPrefix = "key_"

// ID represents a unique API key identifier
type ID string

// NewID generates a new API key ID with prefix
func NewID() ID {
	return ID(IDPrefix + cuid2.Generate())
}

// ParseID validates and parses a string into an ID
func ParseID(s string) (ID, error) {
	if s == "" {
		return "", apperr.Wrap(apperr.Invalid, "validation_failed", errors.New("API key ID is required"))
	}
	if !strings.HasPrefix(s, IDPrefix) {
		return "", apperr.Wrap(apperr.Invalid, "validation_failed", fmt.Errorf("invalid API key ID format: must start with %s", IDPrefix))
	}
	return ID(s), nil
}

// String returns the string representation
func (id ID) String() string {
	return string(id)
}

// IsZero returns true if the ID is empty
func (id ID) IsZero() bool {
	return id == ""
}
