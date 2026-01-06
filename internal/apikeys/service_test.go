package apikeys_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	apikeyshelpers "github.com/kannon-email/kannon/internal/apikeys/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDomain = "test.example.com"

func TestService_CreateKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		name := "test-key"

		key, err := service.CreateKey(ctx, domain, name, nil)
		require.NoError(t, err)
		assert.NotNil(t, key)
		assert.Equal(t, domain, key.Domain())
		assert.Equal(t, name, key.Name())
		assert.False(t, key.ID().IsZero())
		assert.NotZero(t, key.CreatedAt())
		assert.True(t, key.IsActiveStatus())
	})

	t.Run("WithExpiration", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		name := "expiring-key"
		expiresAt := time.Now().Add(24 * time.Hour)

		key, err := service.CreateKey(ctx, domain, name, &expiresAt)
		require.NoError(t, err)
		assert.NotNil(t, key.ExpiresAt())
	})

	t.Run("InvalidDomain", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		_, err := service.CreateKey(ctx, "", "test-key", nil)
		assert.Error(t, err)
	})

	t.Run("InvalidName", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		_, err := service.CreateKey(ctx, testDomain, "", nil)
		assert.Error(t, err)
	})
}

func TestService_GetKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		created, err := service.CreateKey(ctx, domain, "test-key", nil)
		require.NoError(t, err)

		ref := apikeys.NewKeyRef(domain, created.ID())
		found, err := service.GetKey(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, created.ID(), found.ID())
		assert.Equal(t, created.Key(), found.Key())
		assert.Equal(t, created.Name(), found.Name())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		nonExistentID, _ := apikeys.ParseID("key_nonexistent")
		ref := apikeys.NewKeyRef(domain, nonExistentID)

		_, err := service.GetKey(ctx, ref)
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})
}

func TestService_ListKeys(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain

		// Create 3 keys
		for i := 0; i < 3; i++ {
			_, err := service.CreateKey(ctx, domain, "test-key", nil)
			require.NoError(t, err)
		}

		keys, total, err := service.ListKeys(ctx, domain, false, apikeys.Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, keys, 3)
		assert.Equal(t, 3, total)
	})

	t.Run("WithActiveFilter", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain

		// Create 2 active keys
		for i := 0; i < 2; i++ {
			_, err := service.CreateKey(ctx, domain, "active-key", nil)
			require.NoError(t, err)
		}

		// Create 1 inactive key
		inactiveKey, err := service.CreateKey(ctx, domain, "inactive-key", nil)
		require.NoError(t, err)

		ref := apikeys.NewKeyRef(domain, inactiveKey.ID())
		_, err = service.DeactivateKey(ctx, ref)
		require.NoError(t, err)

		// List only active
		activeKeys, activeTotal, err := service.ListKeys(ctx, domain, true, apikeys.Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, activeKeys, 2)
		assert.Equal(t, 2, activeTotal)
		for _, k := range activeKeys {
			assert.True(t, k.IsActiveStatus())
		}

		// List all (including inactive)
		allKeys, allTotal, err := service.ListKeys(ctx, domain, false, apikeys.Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, allKeys, 3)
		assert.Equal(t, 3, allTotal)
	})

	t.Run("WithPagination", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain

		// Create 5 keys
		for i := 0; i < 5; i++ {
			_, err := service.CreateKey(ctx, domain, "test-key", nil)
			require.NoError(t, err)
		}

		// Get first 2
		page1, total1, err := service.ListKeys(ctx, domain, false, apikeys.Pagination{Limit: 2, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, page1, 2)
		assert.Equal(t, 5, total1)

		// Get next 2
		page2, total2, err := service.ListKeys(ctx, domain, false, apikeys.Pagination{Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)
		assert.Equal(t, 5, total2)

		// Get last 1
		page3, total3, err := service.ListKeys(ctx, domain, false, apikeys.Pagination{Limit: 2, Offset: 4})
		require.NoError(t, err)
		assert.Len(t, page3, 1)
		assert.Equal(t, 5, total3)
	})

	t.Run("InvalidDomain", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		_, _, err := service.ListKeys(ctx, "", false, apikeys.Pagination{Limit: 10, Offset: 0})
		assert.Error(t, err)
	})
}

func TestService_DeactivateKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		created, err := service.CreateKey(ctx, domain, "test-key", nil)
		require.NoError(t, err)
		assert.True(t, created.IsActiveStatus())

		ref := apikeys.NewKeyRef(domain, created.ID())
		deactivated, err := service.DeactivateKey(ctx, ref)
		require.NoError(t, err)
		assert.False(t, deactivated.IsActiveStatus())
		assert.NotNil(t, deactivated.DeactivatedAt())

		// Verify deactivation persisted
		fetched, err := service.GetKey(ctx, ref)
		require.NoError(t, err)
		assert.False(t, fetched.IsActiveStatus())
		assert.NotNil(t, fetched.DeactivatedAt())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		nonExistentID, _ := apikeys.ParseID("key_nonexistent")
		ref := apikeys.NewKeyRef(domain, nonExistentID)

		_, err := service.DeactivateKey(ctx, ref)
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})
}

func TestService_ValidateForAuth(t *testing.T) {
	t.Run("ValidKey", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		created, err := service.CreateKey(ctx, domain, "test-key", nil)
		require.NoError(t, err)

		validated, err := service.ValidateForAuth(ctx, domain, created.Key())
		require.NoError(t, err)
		assert.Equal(t, created.ID(), validated.ID())
		assert.True(t, validated.IsActiveStatus())
	})

	t.Run("InactiveKey", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		created, err := service.CreateKey(ctx, domain, "test-key", nil)
		require.NoError(t, err)

		ref := apikeys.NewKeyRef(domain, created.ID())
		_, err = service.DeactivateKey(ctx, ref)
		require.NoError(t, err)

		// Validation should fail for inactive key (returns generic error for security)
		_, err = service.ValidateForAuth(ctx, domain, created.Key())
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})

	t.Run("ExpiredKey", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain
		// Create a key first with valid expiration
		futureTime := time.Now().Add(24 * time.Hour)
		key, err := apikeys.NewAPIKey(domain, "expired-key", &futureTime)
		require.NoError(t, err)

		err = repo.Create(ctx, key)
		require.NoError(t, err)

		// Now create an expired version using LoadAPIKey to bypass validation
		pastTime := time.Now().Add(-24 * time.Hour)
		expiredKey := apikeys.LoadAPIKey(apikeys.LoadAPIKeyParams{
			ID:            key.ID(),
			Key:           key.Key(),
			Name:          key.Name(),
			Domain:        key.Domain(),
			CreatedAt:     key.CreatedAt(),
			ExpiresAt:     &pastTime,
			IsActive:      true,
			DeactivatedAt: nil,
		})

		// Update the repository with the expired key
		ref := apikeys.NewKeyRef(domain, key.ID())
		_, err = repo.Update(ctx, ref, func(k *apikeys.APIKey) error {
			*k = *expiredKey
			return nil
		})
		require.NoError(t, err)

		// Validation should fail for expired key (returns generic error for security)
		_, err = service.ValidateForAuth(ctx, domain, key.Key())
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain

		// Returns generic error for security
		_, err := service.ValidateForAuth(ctx, domain, "k_nonexistent12345678901234567890")
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})

	t.Run("InvalidKeyFormat", func(t *testing.T) {
		ctx := context.Background()
		repo := apikeyshelpers.NewInMemoryRepository()
		service := apikeys.NewService(repo)

		domain := testDomain

		// Returns generic error for security
		_, err := service.ValidateForAuth(ctx, domain, "invalid-key")
		assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
	})
}
