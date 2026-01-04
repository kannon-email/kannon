package tests

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/stretchr/testify/assert"
)

// DomainWithKey represents a test domain with an API key for authentication
type DomainWithKey struct {
	Domain *adminv1.Domain
	APIKey string
}

// CreateTestDomain creates a test domain with an API key via the admin API
func CreateTestDomain(t *testing.T, adminAPI apiv1connect.ApiHandler) *DomainWithKey {
	t.Helper()

	// Create domain
	res, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminv1.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)

	// Create an API key for authentication
	keyRes, err := adminAPI.CreateAPIKey(context.Background(), connect.NewRequest(&adminv1.CreateAPIKeyRequest{
		Domain: res.Msg.Domain,
		Name:   "test-key",
	}))
	assert.Nil(t, err)

	return &DomainWithKey{
		Domain: res.Msg,
		APIKey: keyRes.Msg.ApiKey.Key,
	}
}
