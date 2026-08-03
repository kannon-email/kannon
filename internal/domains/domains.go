// Package domains defines the SenderDomain domain entity per CONTEXT.md: the sender-tenant
// identity (domain name + DKIM key pair) under which Batches are authored and mail is signed. The
// Go type is Domain for historical reasons; storage row sqlc.Domain, wire payload proto Domain.
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

// Domain is the SenderDomain entity: a sender-tenant identified by its domain
// name and the DKIM key pair used to sign outgoing mail for it.
type Domain struct {
	id             int32
	domain         values.DomainName
	dkimPrivateKey string
	dkimPublicKey  string
	createdAt      time.Time
	tracking       tracking.Policy
}

// New creates a new SenderDomain with a freshly generated DKIM key pair. The numeric id, createdAt
// and Tracking Policy are populated by the repository on Create, so the starting Policy is stated
// only by the column default. The name needs no check: only Parse can have produced it.
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

func (d *Domain) ID() int32              { return d.id }
func (d *Domain) DkimPrivateKey() string { return d.dkimPrivateKey }
func (d *Domain) DkimPublicKey() string  { return d.dkimPublicKey }
func (d *Domain) CreatedAt() time.Time   { return d.createdAt }

// Name is the Domain's canonical domain name. It is what a Repository, and
// later an authorization Anchor, is addressed with: both must be unable to
// receive anything but a canonical name.
func (d *Domain) Name() values.DomainName { return d.domain }

// Domain renders the domain name for the wire, for logs and for the string-shaped places it is
// interpolated (a Batch id, an email address, a DKIM selector). It exists beside Name so that
// rendering is never a reason to hand a bare string to something that should demand the type.
func (d *Domain) Domain() string { return d.domain.String() }

// TrackingPolicy is the Domain's Tracking Policy: the ceiling every Batch and
// Recipient of this Domain is resolved against.
func (d *Domain) TrackingPolicy() tracking.Policy { return d.tracking }

// Pb translates to the proto wire type. Only the domain name, the public DKIM
// key and the Tracking Policy are exposed on the wire — the private key never
// leaves the server.
func (d *Domain) Pb() *pb.Domain {
	return &pb.Domain{
		Domain:     d.domain.String(),
		DkimPubKey: d.dkimPublicKey,
		Tracking:   trackingpb.FromPolicy(d.tracking),
	}
}
