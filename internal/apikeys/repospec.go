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

		result, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)
		assert.False(t, result.Key.ID().IsZero(), "ID should be populated after create")
		assert.NotZero(t, result.Key.CreatedAt(), "CreatedAt should be populated")
	})

	t.Run("WithExpiration", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		expiresAt := time.Now().Add(24 * time.Hour)
		result, err := NewAPIKey(domain, "expiring-key", &expiresAt)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)
		assert.NotNil(t, result.Key.ExpiresAt())
	})
}

func testUpdate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Deactivate", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create a key
		result, err := NewAPIKey(domain, "to-deactivate", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)

		// Deactivate using transactional update
		ref := NewKeyRef(domain, result.Key.ID())
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

	t.Run("PreservesStateOnError", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		// Create a key
		result, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)

		// Verify key is initially active
		ref := NewKeyRef(domain, result.Key.ID())
		initial, err := repo.GetByID(ctx, ref)
		require.NoError(t, err)
		assert.True(t, initial.IsActiveStatus(), "key should be active initially")
		assert.Nil(t, initial.DeactivatedAt(), "deactivatedAt should be nil initially")

		// Attempt update that mutates key then returns error
		testErr := fmt.Errorf("intentional test error")
		_, err = repo.Update(ctx, ref, func(k *APIKey) error {
			k.Deactivate() // Mutates the key
			return testErr // But then fails
		})
		require.ErrorIs(t, err, testErr)

		// Verify the stored key was NOT mutated
		fetched, err := repo.GetByID(ctx, ref)
		require.NoError(t, err)
		assert.True(t, fetched.IsActiveStatus(), "key should still be active after failed update")
		assert.Nil(t, fetched.DeactivatedAt(), "deactivatedAt should still be nil after failed update")
	})
}

func testGetByKey(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		result, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)

		found, err := repo.GetByKey(ctx, domain, result.PlaintextKey)
		require.NoError(t, err)
		assert.Equal(t, result.Key.ID(), found.ID())
		assert.Equal(t, result.Key.KeyHash(), found.KeyHash())
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

		result, err := NewAPIKey(domain, "test-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, result.Key)
		require.NoError(t, err)

		ref := NewKeyRef(domain, result.Key.ID())
		found, err := repo.GetByID(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, result.Key.ID(), found.ID())
		assert.Equal(t, result.Key.KeyHash(), found.KeyHash())
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
			result, err := NewAPIKey(domain, fmt.Sprintf("key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, result.Key)
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
			result, err := NewAPIKey(domain, fmt.Sprintf("active-key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, result.Key)
			require.NoError(t, err)
		}

		// Create 1 inactive key
		inactiveResult, err := NewAPIKey(domain, "inactive-key", nil)
		require.NoError(t, err)

		err = repo.Create(ctx, inactiveResult.Key)
		require.NoError(t, err)

		ref := NewKeyRef(domain, inactiveResult.Key.ID())
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
			result, err := NewAPIKey(domain, fmt.Sprintf("key-%d", i), nil)
			require.NoError(t, err)

			err = repo.Create(ctx, result.Key)
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
