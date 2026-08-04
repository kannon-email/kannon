package tests

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/authz"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/stretchr/testify/assert"
)

// DomainWithKey represents a test domain with an API key for authentication
type DomainWithKey struct {
	Domain *adminv1.Domain
	APIKey string
}

// AdminContext returns ctx carrying admin on the root — the authority the Admin API's token
// confers, built from the vocabulary rather than from a token, since no credential authenticated
// anything here. A test holding a Connect handler has run no interceptor, so without this every
// guarded operation refuses; such tests prove nothing about authorization, which the tables in
// internal/authz and internal/authzconnect cover instead.
func AdminContext(ctx context.Context) context.Context {
	return authz.NewContext(ctx, authz.MustNewPrincipal("test-admin",
		authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor())))
}

// CreateTestDomain creates a test domain with an API key via the admin API
func CreateTestDomain(t *testing.T, adminAPI apiv1connect.ApiHandler) *DomainWithKey {
	t.Helper()

	domain := FakeDomain(t)
	ctx := AdminContext(context.Background())

	res, err := adminAPI.CreateDomain(ctx, connect.NewRequest(&adminv1.CreateDomainRequest{
		Domain: domain,
	}))
	assert.Nil(t, err)

	// Create an API key for authentication
	keyRes, err := adminAPI.CreateAPIKey(ctx, connect.NewRequest(&adminv1.CreateAPIKeyRequest{
		Domain: res.Msg.Domain,
		Name:   "test-key",
	}))
	assert.Nil(t, err)

	return &DomainWithKey{
		Domain: res.Msg,
		APIKey: keyRes.Msg.Key,
	}
}
