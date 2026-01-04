package apikeys

// KeyRef is an interface for referencing an API key (domain + ID combination)
type KeyRef interface {
	Domain() string
	KeyID() ID
}

// keyRef is the concrete implementation of KeyRef
type keyRef struct {
	domain string
	id     ID
}

// Domain returns the domain name
func (r keyRef) Domain() string {
	return r.domain
}

// KeyID returns the key ID as a string
func (r keyRef) KeyID() ID {
	return r.id
}

// NewKeyRef creates a new KeyRef
func NewKeyRef(domain string, id ID) KeyRef {
	return keyRef{domain: domain, id: id}
}

// ParseKeyRef validates and creates a KeyRef from strings
func ParseKeyRef(domain, id string) (KeyRef, error) {
	if err := validateDomain(domain); err != nil {
		return nil, err
	}
	parsedID, err := ParseID(id)
	if err != nil {
		return nil, err
	}
	return keyRef{domain: domain, id: parsedID}, nil
}
