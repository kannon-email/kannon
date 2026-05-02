package templates

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RepoTestHelper provides test utilities for repository spec tests.
type RepoTestHelper interface {
	// CreateDomain creates a fresh domain row, registers cleanup, and
	// returns the domain name.
	CreateDomain(t *testing.T) string
}

// RunRepoSpec exercises any Repository implementation against the
// documented behaviour. Implementations must pass every sub-test.
func RunRepoSpec(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Create", func(t *testing.T) { testCreate(t, repo, helper) })
	t.Run("Update", func(t *testing.T) { testUpdate(t, repo, helper) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, repo, helper) })
	t.Run("GetByID", func(t *testing.T) { testGetByID(t, repo, helper) })
	t.Run("FindByDomain", func(t *testing.T) { testFindByDomain(t, repo, helper) })
	t.Run("ListAndCount", func(t *testing.T) { testListAndCount(t, repo, helper) })
}

func testCreate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Persistent", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewPersistent(domain, "<p>hi {{name}}</p>", "Greeting")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		fetched, err := repo.GetByID(ctx, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, tpl.TemplateID(), fetched.TemplateID())
		assert.Equal(t, "<p>hi {{name}}</p>", fetched.Html())
		assert.Equal(t, "Greeting", fetched.Title())
		assert.Equal(t, TypePersistent, fetched.Type())
		assert.False(t, fetched.CreatedAt().IsZero())
	})

	t.Run("Transient", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewTransient(domain, "<p>once</p>")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		fetched, err := repo.GetByID(ctx, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, TypeTransient, fetched.Type())
	})
}

func testUpdate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewPersistent(domain, "<p>v1</p>", "old")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		updated, err := repo.Update(ctx, tpl.TemplateID(), func(t *Template) error {
			t.SetHTML("<p>v2</p>")
			t.SetTitle("new")
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "<p>v2</p>", updated.Html())
		assert.Equal(t, "new", updated.Title())

		fetched, err := repo.GetByID(ctx, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, "<p>v2</p>", fetched.Html())
		assert.Equal(t, "new", fetched.Title())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.Update(ctx, "template_missing@nowhere.test", func(*Template) error { return nil })
		assert.ErrorIs(t, err, ErrTemplateNotFound)
	})
}

func testDelete(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewPersistent(domain, "<p>x</p>", "doomed")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		deleted, err := repo.Delete(ctx, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, tpl.TemplateID(), deleted.TemplateID())

		_, err = repo.GetByID(ctx, tpl.TemplateID())
		assert.ErrorIs(t, err, ErrTemplateNotFound)
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.Delete(ctx, "template_missing@nowhere.test")
		assert.ErrorIs(t, err, ErrTemplateNotFound)
	})
}

func testGetByID(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewPersistent(domain, "<p>hi</p>", "t")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		fetched, err := repo.GetByID(ctx, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, tpl.TemplateID(), fetched.TemplateID())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.GetByID(ctx, "template_missing@nowhere.test")
		assert.ErrorIs(t, err, ErrTemplateNotFound)
	})
}

func testFindByDomain(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		tpl, err := NewPersistent(domain, "<p>hi</p>", "t")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		fetched, err := repo.FindByDomain(ctx, domain, tpl.TemplateID())
		require.NoError(t, err)
		assert.Equal(t, tpl.TemplateID(), fetched.TemplateID())
	})

	t.Run("WrongDomain", func(t *testing.T) {
		ctx := t.Context()
		domainA := helper.CreateDomain(t)
		domainB := helper.CreateDomain(t)

		tpl, err := NewPersistent(domainA, "<p>hi</p>", "t")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, tpl))

		_, err = repo.FindByDomain(ctx, domainB, tpl.TemplateID())
		assert.ErrorIs(t, err, ErrTemplateNotFound)
	})
}

func testListAndCount(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("OnlyPersistent", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		for i := range 3 {
			tpl, err := NewPersistent(domain, "<p>x</p>", fmt.Sprintf("t-%d", i))
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, tpl))
		}

		// Transient templates do not appear in List/Count.
		transient, err := NewTransient(domain, "<p>once</p>")
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, transient))

		listed, err := repo.List(ctx, domain, Pagination{Skip: 0, Take: 10})
		require.NoError(t, err)
		assert.Len(t, listed, 3)

		total, err := repo.Count(ctx, domain)
		require.NoError(t, err)
		assert.Equal(t, 3, total)
	})

	t.Run("Pagination", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)

		for i := range 5 {
			tpl, err := NewPersistent(domain, "<p>x</p>", fmt.Sprintf("t-%d", i))
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, tpl))
		}

		page1, err := repo.List(ctx, domain, Pagination{Skip: 0, Take: 2})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := repo.List(ctx, domain, Pagination{Skip: 2, Take: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)

		page3, err := repo.List(ctx, domain, Pagination{Skip: 4, Take: 2})
		require.NoError(t, err)
		assert.Len(t, page3, 1)
	})
}
