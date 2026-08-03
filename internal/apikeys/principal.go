package apikeys

import (
	"fmt"

	"github.com/kannon-email/kannon/internal/authz"
)

// Principal resolves this API Key to the authority it confers: sender, anchored on the key's own
// Domain, and nothing else (ADR 0008) — no Attribution, identifier "<key-id>@<domain>". An error
// means a corrupt row whose Domain cannot carry an Anchor, so the caller must refuse the request.
func (k *APIKey) Principal() (authz.Principal, error) {
	grant, err := authz.NewGrant(authz.RoleSender, authz.DomainAnchor(k.DomainName()))
	if err != nil {
		return authz.Principal{}, fmt.Errorf("api key %s cannot be granted sender on its own domain: %w", k.ID(), err)
	}
	return authz.NewPrincipal(k.ID().String()+"@"+k.DomainName().String(), grant)
}
