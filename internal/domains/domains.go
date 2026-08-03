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
	"time"

	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/values"
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
	domain         values.DomainName
	dkimPrivateKey string
	dkimPublicKey  string
	createdAt      time.Time
	tracking       tracking.Policy
}

// New creates a new SenderDomain with a freshly generated DKIM key pair.
// The numeric id, createdAt and Tracking Policy are populated by the repository
// on Create — the starting Policy is the column default, so it is stated in one
// place only.
//
// There is no check that the FQDN is present and well formed: values.DomainName cannot
// be built by conversion, so anything that reaches here has already been
// canonicalised and validated by values.Parse.
func New(domain values.DomainName) (*Domain, error) {
	keys, err := dkim.GenerateDKIMKeysPair()
	if err != nil {
		return nil, err
	}
	return &Domain{
		domain:         domain,
		dkimPrivateKey: keys.PrivateKey,
		dkimPublicKey:  keys.PublicKey,
	}, nil
}

// LoadParams contains all fields needed to rehydrate a Domain from storage.
type LoadParams struct {
	ID             int32
	Domain         values.DomainName
	DkimPrivateKey string
	DkimPublicKey  string
	CreatedAt      time.Time
	Tracking       tracking.Policy
}

// Load rehydrates a Domain from stored data (used by repository implementations).
func Load(p LoadParams) *Domain {
	return &Domain{
		id:             p.ID,
		domain:         p.Domain,
		dkimPrivateKey: p.DkimPrivateKey,
		dkimPublicKey:  p.DkimPublicKey,
		createdAt:      p.CreatedAt,
		tracking:       p.Tracking,
	}
}

// Getters

func (d *Domain) ID() int32              { return d.id }
func (d *Domain) DkimPrivateKey() string { return d.dkimPrivateKey }
func (d *Domain) DkimPublicKey() string  { return d.dkimPublicKey }
func (d *Domain) CreatedAt() time.Time   { return d.createdAt }

// FQDN is the Domain's canonical fully qualified domain name. It is what a
// Repository, and later an authorization Anchor, is addressed with: both must
// be unable to receive anything but a canonical FQDN.
func (d *Domain) Name() values.DomainName { return d.domain }

// Domain renders the FQDN for the wire, for logs and for the many string-shaped
// places an FQDN is interpolated (a Batch id, an email address, a DKIM
// selector). It exists beside FQDN so that rendering is never a reason to hand
// a bare string to something that should demand the type.
func (d *Domain) Domain() string { return d.domain.String() }

// TrackingPolicy is the Domain's Tracking Policy: the ceiling every Batch and
// Recipient of this Domain is resolved against.
func (d *Domain) TrackingPolicy() tracking.Policy { return d.tracking }

// Pb translates to the proto wire type. Only the FQDN, the public DKIM key and
// the Tracking Policy are exposed on the wire — the private key never leaves
// the server.
func (d *Domain) Pb() *pb.Domain {
	return &pb.Domain{
		Domain:     d.domain.String(),
		DkimPubKey: d.dkimPublicKey,
		Tracking:   trackingpb.FromPolicy(d.tracking),
	}
}
