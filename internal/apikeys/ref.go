package apikeys

import (
	"github.com/kannon-email/kannon/internal/values"
)

// KeyRef references an API key by Domain and ID. The Domain is a canonical name rather than a
// string: every lookup through a KeyRef is domain-scoped, and a spelling that never went through
// values.Parse would answer "not found" for a key that does exist.
type KeyRef interface {
	DomainName() values.DomainName
	KeyID() ID
}

type keyRef struct {
	domain values.DomainName
	id     ID
}

func (r keyRef) DomainName() values.DomainName {
	return r.domain
}

func (r keyRef) KeyID() ID {
	return r.id
}

func NewKeyRef(domain values.DomainName, id ID) KeyRef {
	return keyRef{domain: domain, id: id}
}

// ParseKeyRef validates and creates a KeyRef from the two strings a request carries. It is the
// boundary at which a wire-supplied domain becomes a canonical name, so nothing downstream has to
// wonder whether the value was canonicalised.
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
