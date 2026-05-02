// Package domains defines the SenderDomain domain entity per CONTEXT.md:
// the sender-tenant identity (FQDN + DKIM key pair) under which Batches are
// authored and emails are signed. The Go type is named Domain for historical
// reasons; renaming the wire/DB-visible field "domain" to "fqdn" is wire/DB
// breaking and deferred to the refactoring backlog.
//
// Storage row is sqlc.Domain; the on-the-wire payload is the proto Domain
// (which exposes only Domain + DkimPubKey); the domain entity is
// domains.Domain.
package domains

import (
	"errors"
	"fmt"
	"time"

	"github.com/kannon-email/kannon/internal/dkim"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
)

// Domain errors.
var (
	ErrDomainNotFound = errors.New("domain not found")
)

// Domain is the SenderDomain entity: a sender-tenant identified by its FQDN
// and the DKIM key pair used to sign outgoing mail for it.
type Domain struct {
	id             int32
	domain         string
	dkimPrivateKey string
	dkimPublicKey  string
	createdAt      time.Time
}

// New creates a new SenderDomain with a freshly generated DKIM key pair.
// The numeric id and createdAt are populated by the repository on Create.
func New(fqdn string) (*Domain, error) {
	if fqdn == "" {
		return nil, fmt.Errorf("domain is required")
	}
	keys, err := dkim.GenerateDKIMKeysPair()
	if err != nil {
		return nil, err
	}
	return &Domain{
		domain:         fqdn,
		dkimPrivateKey: keys.PrivateKey,
		dkimPublicKey:  keys.PublicKey,
	}, nil
}

// LoadParams contains all fields needed to rehydrate a Domain from storage.
type LoadParams struct {
	ID             int32
	Domain         string
	DkimPrivateKey string
	DkimPublicKey  string
	CreatedAt      time.Time
}

// Load rehydrates a Domain from stored data (used by repository implementations).
func Load(p LoadParams) *Domain {
	return &Domain{
		id:             p.ID,
		domain:         p.Domain,
		dkimPrivateKey: p.DkimPrivateKey,
		dkimPublicKey:  p.DkimPublicKey,
		createdAt:      p.CreatedAt,
	}
}

// Getters

func (d *Domain) ID() int32              { return d.id }
func (d *Domain) Domain() string         { return d.domain }
func (d *Domain) DkimPrivateKey() string { return d.dkimPrivateKey }
func (d *Domain) DkimPublicKey() string  { return d.dkimPublicKey }
func (d *Domain) CreatedAt() time.Time   { return d.createdAt }

// Pb translates to the proto wire type. Only the FQDN and the public DKIM
// key are exposed on the wire — the private key never leaves the server.
func (d *Domain) Pb() *pb.Domain {
	return &pb.Domain{
		Domain:     d.domain,
		DkimPubKey: d.dkimPublicKey,
	}
}
