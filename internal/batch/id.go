package batch

import (
	"fmt"
	"strings"

	"github.com/lucsky/cuid"
)

const IDPrefix = "msg_"

// ID represents a unique Batch identifier in the form "msg_<cuid>@<domain>".
type ID string

// NewID generates a new Batch ID for the given domain.
func NewID(domain string) ID {
	return ID(fmt.Sprintf("%s%s@%s", IDPrefix, cuid.New(), domain))
}

// ParseID validates and parses a string into an ID.
func ParseID(s string) (ID, error) {
	if s == "" {
		return "", fmt.Errorf("batch ID is required")
	}
	if !strings.HasPrefix(s, IDPrefix) {
		return "", fmt.Errorf("invalid batch ID format: must start with %s", IDPrefix)
	}
	if !strings.Contains(s, "@") {
		return "", fmt.Errorf("invalid batch ID format: missing domain")
	}
	return ID(s), nil
}

// String returns the string representation.
func (id ID) String() string {
	return string(id)
}

// IsZero returns true if the ID is empty.
func (id ID) IsZero() bool {
	return id == ""
}
