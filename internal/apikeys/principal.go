package apikeys

import (
	"fmt"

	"github.com/kannon-email/kannon/internal/authz"
)

// Principal resolves immutable roles on the credential's own Domain, retaining its identity.
// Missing legacy role metadata resolves to sender. No Attribution is inferred.
func (k *APIKey) Principal() (authz.Principal, error) {
	grants := []authz.Grant{}
	for _, role := range k.Roles() {
		grant, err := authz.NewGrant(role, authz.DomainAnchor(k.DomainName()))
		if err != nil {
			return authz.Principal{}, fmt.Errorf("api key %s cannot be granted authority on its own domain: %w", k.ID(), err)
		}
		grants = append(grants, grant)
	}
	return authz.NewPrincipal(k.ID().String()+"@"+k.DomainName().String(), grants...)
}
