package ref

import (
	"fmt"

	"github.com/ludusrusso/kannon/pkg/values/fqdn"
	"github.com/ludusrusso/kannon/pkg/values/id"
)

type ref struct {
	id   id.ID
	fqdn fqdn.FQDN
}

type Ref interface {
	ID() id.ID
	FQDN() fqdn.FQDN
	String() string
}

func (r ref) ID() id.ID {
	return r.id
}

func (r ref) FQDN() fqdn.FQDN {
	return r.fqdn
}

func (r ref) String() string {
	return fmt.Sprintf("%s@%s", r.id, r.fqdn)
}

func NewRef(prefix string, fqdn fqdn.FQDN) (Ref, error) {
	i, err := id.CreateID(prefix)
	if err != nil {
		return nil, err
	}

	return ref{
		id:   i,
		fqdn: fqdn,
	}, nil
}
