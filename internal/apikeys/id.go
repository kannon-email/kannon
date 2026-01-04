package apikeys

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/lucsky/cuid"
)

const IDPrefix = "key_"

// ID represents a unique API key identifier
type ID string

// NewID generates a new API key ID with prefix
func NewID() (ID, error) {
	c, err := cuid.NewCrypto(rand.Reader)
	if err != nil {
		return "", err
	}
	return ID(IDPrefix + c), nil
}

// ParseID validates and parses a string into an ID
func ParseID(s string) (ID, error) {
	if s == "" {
		return "", fmt.Errorf("API key ID is required")
	}
	if !strings.HasPrefix(s, IDPrefix) {
		return "", fmt.Errorf("invalid API key ID format: must start with %s", IDPrefix)
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
