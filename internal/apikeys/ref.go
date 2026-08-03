package apikeys

import (
	"github.com/kannon-email/kannon/internal/values"
)

// KeyRef is an interface for referencing an API key (domain + ID combination).
//
// The Domain is named by its canonical FQDN rather than a string: every lookup
// through a KeyRef is scoped by domain, and a spelling that never went through
// values.Parse would answer "not found" for a key that does exist.
type KeyRef interface {
	DomainName() values.DomainName
	KeyID() ID
}

// keyRef is the concrete implementation of KeyRef
type keyRef struct {
	domain values.DomainName
	id     ID
}

// FQDN returns the Domain the referenced key belongs to
func (r keyRef) DomainName() values.DomainName {
	return r.domain
}

// KeyID returns the key ID as a string
func (r keyRef) KeyID() ID {
	return r.id
}

// NewKeyRef creates a new KeyRef
func NewKeyRef(domain values.DomainName, id ID) KeyRef {
	return keyRef{domain: domain, id: id}
}

// ParseKeyRef validates and creates a KeyRef from the two strings a request
// carries. It is the boundary at which a wire-supplied domain becomes an FQDN,
// so nothing downstream has to wonder whether the value was canonicalised.
func ParseKeyRef(domain, id string) (KeyRef, error) {
	parsedDomain, err := values.Parse(domain)
	if err != nil {
		return nil, err
	}
	parsedID, err := ParseID(id)
	if err != nil {
		return nil, err
	}
	return keyRef{domain: parsedDomain, id: parsedID}, nil
}
