package apikeys

import (
	"fmt"

	"github.com/kannon-email/kannon/internal/authz"
)

// Principal resolves this API Key to the authority it confers: sender, anchored
// on the key's own Domain, and nothing else.
//
// This is the key-to-Principal adapter ADR 0008 names. The authority is assembled
// here rather than stored, and it is exactly one Grant: the Role's one rule pins
// the kind (a Domain's Batches) and the Anchor pins the place, so the holder can
// send for its Domain and can do nothing else — it cannot read that Domain,
// rewrite its Templates, mint a second key, list its siblings, or send for anybody
// else. Keys for reading statistics or administering a Domain need a grants table
// and are deferred; the deferral costs little precisely because what such a table
// will store is what a Grant already is — a Role name and an Anchor — so this
// function is the only code that will change, and no schema does.
//
// The Principal carries no Attribution, and deliberately not as a check somebody
// could forget to write: sender does not hold Attribute (see authz's catalogue),
// so an Attribution set on it would make Guard refuse the operation rather than
// record a name nothing verified. There is no trusted front-end behind an API Key
// for it to speak on behalf of (ADR 0008), and a key that could name a person
// would let its holder write any name into the record of who did what.
//
// The identifier names the credential rather than its Domain — "<key-id>@<fqdn>" —
// because the Grant is identical for every key of one Domain and the identifier is
// the only thing that tells two of them apart in a log or in whatever eventually
// records who acted. The "@" cannot be ambiguous: an FQDN may not contain one
// (internal/values), so the identifier splits back to the key and the Domain it
// belongs to.
//
// An error means the key's Domain cannot carry an Anchor at all, which is a
// corrupt row rather than bad input — a zero FQDN reaching DomainAnchor composes
// an Anchor with an empty segment, which NewGrant refuses. The caller must refuse
// the request, not proceed with a Principal holding less: a send would then be
// turned away by a guard reporting the caller's From address as unauthorized,
// which is true of nothing and would send an operator looking in the wrong place.
func (k *APIKey) Principal() (authz.Principal, error) {
	grant, err := authz.NewGrant(authz.RoleSender, authz.DomainAnchor(k.DomainName()))
	if err != nil {
		return authz.Principal{}, fmt.Errorf("api key %s cannot be granted sender on its own domain: %w", k.ID(), err)
	}
	return authz.NewPrincipal(k.ID().String()+"@"+k.DomainName().String(), grant)
}
