package apikeys

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RepoTestHelper provides test utilities for repository tests
type RepoTestHelper interface {
	CreateDomain(t *testing.T) string // Creates a domain, registers cleanup, and returns its name
}

// RunRepoSpec runs the repository specification tests against any Repository implementation
func RunRepoSpec(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Create", func(t *testing.T) {
		testCreate(t, repo, helper)
	})
	t.Run("Update", func(t *testing.T) {
		testUpdate(t, repo, helper)
	})
	t.Run("GetByKey", func(t *testing.T) {
		testGetByKey(t, repo, helper)
	})
	t.Run("GetByID", func(t *testing.T) {
		testGetByID(t, repo, helper)
	})
	t.Run("List", func(t *testing.T) {
		testList(t, repo, helper)
	})
}

func testCreate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		key, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, key)
		require.NoError(t, err)
		assert.False(t, key.ID().IsZero(), "ID should be populated after create")
		assert.NotZero(t, key.CreatedAt(), "CreatedAt should be populated")
	})

	t.Run("WithExpiration", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		expiresAt := time.Now().Add(24 * time.Hour)
		key, err := NewAPIKey(domain, "expiring-key", &expiresAt)
		require.NoError(t, err)

		err = repo.Create(ctx, key)
		require.NoError(t, err)
		assert.NotNil(t, key.ExpiresAt())
	})
}

func testUpdate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Deactivate", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create a key
		key, err := NewAPIKey(domain, "to-deactivate", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, key)
		require.NoError(t, err)

		// Deactivate using transactional update
		ref := NewKeyRef(domain, key.ID())
		updated, err := repo.Update(ctx, ref, func(k *APIKey) error {
			k.Deactivate()
			return nil
		})
		require.NoError(t, err)
		assert.False(t, updated.IsActiveStatus())
		assert.NotNil(t, updated.DeactivatedAt())

		// Verify deactivation persisted
		fetched, err := repo.GetByID(ctx, ref)
		require.NoError(t, err)
		assert.False(t, fetched.IsActiveStatus())
		assert.NotNil(t, fetched.DeactivatedAt())
	})
}

func testGetByKey(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		created, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		keyValue := created.Key()

		err = repo.Create(ctx, created)
		require.NoError(t, err)

		found, err := repo.GetByKey(ctx, domain, keyValue)
		require.NoError(t, err)
		assert.Equal(t, created.ID(), found.ID())
		assert.Equal(t, keyValue, found.Key())
		assert.Equal(t, domain, found.Domain())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		_, err := repo.GetByKey(ctx, domain, "k_nonexistent12345678901234567890")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Returns ErrKeyNotFound instead of ErrInvalidKey to prevent timing attacks
		_, err := repo.GetByKey(ctx, domain, "invalid-key")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})
}

func testGetByID(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		created, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, created)
		require.NoError(t, err)

		ref := NewKeyRef(domain, created.ID())
		found, err := repo.GetByID(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, created.ID(), found.ID())
		assert.Equal(t, created.Key(), found.Key())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		nonExistentID, _ := ParseID("key_nonexistent")
		ref := NewKeyRef(domain, nonExistentID)
		_, err := repo.GetByID(ctx, ref)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})
}

func testList(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create multiple keys
		for i := range 3 {
			key, err := NewAPIKey(domain, fmt.Sprintf("key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, key)
			require.NoError(t, err)
		}

		keys, err := repo.List(ctx, domain, ListFilters{}, Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, keys, 3)
	})

	t.Run("WithActiveFilter", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create 2 active keys
		for i := range 2 {
			key, err := NewAPIKey(domain, fmt.Sprintf("active-key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, key)
			require.NoError(t, err)
		}

		// Create 1 inactive key
		inactiveKey, err := NewAPIKey(domain, "inactive-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, inactiveKey)
		require.NoError(t, err)

		ref := NewKeyRef(domain, inactiveKey.ID())
		_, err = repo.Update(ctx, ref, func(k *APIKey) error {
			k.Deactivate()
			return nil
		})
		require.NoError(t, err)

		// List only active
		keys, err := repo.List(ctx, domain, ListFilters{OnlyActive: true}, Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, keys, 2)
		for _, k := range keys {
			assert.True(t, k.IsActiveStatus())
		}

		// List all (including inactive)
		allKeys, err := repo.List(ctx, domain, ListFilters{OnlyActive: false}, Pagination{Limit: 10, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, allKeys, 3)
	})

	t.Run("WithPagination", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create 5 keys
		for i := range 5 {
			key, err := NewAPIKey(domain, fmt.Sprintf("key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, key)
			require.NoError(t, err)
		}

		// Get first 2
		page1, err := repo.List(ctx, domain, ListFilters{}, Pagination{Limit: 2, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		// Get next 2
		page2, err := repo.List(ctx, domain, ListFilters{}, Pagination{Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)

		// Get last 1
		page3, err := repo.List(ctx, domain, ListFilters{}, Pagination{Limit: 2, Offset: 4})
		require.NoError(t, err)
		assert.Len(t, page3, 1)
	})
}
