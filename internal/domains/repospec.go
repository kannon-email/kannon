package domains

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunRepoSpec exercises any Repository implementation against the documented
// behaviour. Implementations must pass every sub-test.
func RunRepoSpec(t *testing.T, repo Repository) {
	t.Run("Create", func(t *testing.T) { testCreate(t, repo) })
	t.Run("FindByName", func(t *testing.T) { testFindByName(t, repo) })
	t.Run("List", func(t *testing.T) { testList(t, repo) })
	t.Run("ListPage", func(t *testing.T) { testListPage(t, repo) })
	t.Run("SetTrackingPolicy", func(t *testing.T) { testSetTrackingPolicy(t, repo) })
}

// freshName mints a domain name no other test run uses. MustParse is right
// here: the shape is a constant of this file, so a failure is a bug in the spec
// rather than input.
func freshName(prefix string) values.DomainName {
	return values.MustParse(fmt.Sprintf("%s-%d.test", prefix, time.Now().UnixNano()))
}

// testCreate no longer covers a missing domain name: New takes a
// values.DomainName, which cannot be empty unless it is the zero value, and
// internal/values owns the tests for what Parse refuses.
func testCreate(t *testing.T, repo Repository) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		name := freshName("create")

		d, err := New(name)
		require.NoError(t, err)
		assert.NotEmpty(t, d.DkimPrivateKey(), "New should generate a DKIM private key")
		assert.NotEmpty(t, d.DkimPublicKey(), "New should generate a DKIM public key")

		require.NoError(t, repo.Create(ctx, d))
		assert.NotZero(t, d.ID(), "ID should be populated after create")
		assert.False(t, d.CreatedAt().IsZero(), "CreatedAt should be populated after create")

		fetched, err := repo.FindByName(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, d.ID(), fetched.ID())
		assert.Equal(t, name, fetched.Name())
		assert.Equal(t, d.DkimPublicKey(), fetched.DkimPublicKey())
		assert.Equal(t, d.DkimPrivateKey(), fetched.DkimPrivateKey())
	})

	// A new Domain's ceiling is identified on both axes. New states no Policy, so the Repository is
	// what decides it, and every implementation has to decide the same thing.
	t.Run("StartsWithAnIdentifiedCeiling", func(t *testing.T) {
		ctx := t.Context()
		d, err := New(freshName("initial"))
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		want := tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified}
		assert.Equal(t, want, d.TrackingPolicy())
		fetched, err := repo.FindByName(ctx, d.Name())
		require.NoError(t, err)
		assert.Equal(t, want, fetched.TrackingPolicy())
	})

	t.Run("Duplicate", func(t *testing.T) {
		ctx := t.Context()
		name := freshName("duplicate")

		first, err := New(name)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, first))

		second, err := New(name)
		require.NoError(t, err)
		assert.ErrorIs(t, repo.Create(ctx, second), ErrDomainAlreadyExists)
	})
}

func testFindByName(t *testing.T, repo Repository) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		name := freshName("find")

		d, err := New(name)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		fetched, err := repo.FindByName(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, name, fetched.Name())
	})

	// A Domain created under one spelling is found under any other, since the
	// domain name reaching the repository is canonical by construction.
	t.Run("SucceedsForACaseDifferingSpelling", func(t *testing.T) {
		ctx := t.Context()
		name := freshName("case")

		d, err := New(name)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		upper, err := values.Parse(strings.ToUpper(name.String()))
		require.NoError(t, err)

		fetched, err := repo.FindByName(ctx, upper)
		require.NoError(t, err)
		assert.Equal(t, name, fetched.Name())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.FindByName(ctx, freshName("missing"))
		assert.ErrorIs(t, err, ErrDomainNotFound)
	})
}

// testSetTrackingPolicy asserts the one invariant only observable at the repository boundary: a Mode
// that states nothing never survives at rest on a Domain, because the Domain is the ceiling and a
// ceiling that states nothing would be unenforceable.
func testSetTrackingPolicy(t *testing.T, repo Repository) {
	t.Run("UnstatedModeIsNormalisedToOff", func(t *testing.T) {
		ctx := t.Context()
		name := freshName("tracking")

		d, err := New(name)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		// Only opens is stated; links states nothing.
		updated, err := repo.SetTrackingPolicy(ctx, name, tracking.Policy{Opens: tracking.ModeAnonymous})
		require.NoError(t, err)

		want := tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff}
		assert.Equal(t, want, updated.TrackingPolicy())

		fetched, err := repo.FindByName(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, want, fetched.TrackingPolicy(), "an unstated Mode must not survive at rest")
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := repo.SetTrackingPolicy(t.Context(), freshName("missing"), tracking.Policy{Opens: tracking.ModeOff})
		assert.ErrorIs(t, err, ErrDomainNotFound)
	})
}

func testList(t *testing.T, repo Repository) {
	// List returns Domains in the order they were created, not by name. The names below are
	// created out of alphabetical order so the two cannot be confused.
	t.Run("InCreationOrder", func(t *testing.T) {
		ctx := t.Context()
		prefix := fmt.Sprintf("order-%d-", time.Now().UnixNano())

		var want []values.DomainName
		for _, suffix := range []string{"c", "a", "b"} {
			name := values.MustParse(prefix + suffix + ".test")
			d, err := New(name)
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, d))
			want = append(want, name)
		}

		all, err := repo.List(ctx)
		require.NoError(t, err)

		var got []values.DomainName
		for _, d := range all {
			if strings.HasPrefix(d.Name().String(), prefix) {
				got = append(got, d.Name())
			}
		}
		assert.Equal(t, want, got)
	})

	t.Run("ContainsCreatedDomains", func(t *testing.T) {
		ctx := t.Context()

		want := map[values.DomainName]bool{}
		for i := range 3 {
			name := freshName(fmt.Sprintf("list-%d", i))
			d, err := New(name)
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, d))
			want[name] = true
		}

		all, err := repo.List(ctx)
		require.NoError(t, err)

		got := map[values.DomainName]bool{}
		for _, d := range all {
			got[d.Name()] = true
		}
		for name := range want {
			assert.True(t, got[name], "List should include %q", name)
		}
	})
}

// testListPage walks Domains whose names share a prefix nothing else uses, starting from a cursor
// just below them, so Domains other sub-tests created cannot land between them.
func testListPage(t *testing.T, repo Repository) {
	t.Run("AscendingAndStrictlyAfterTheCursor", func(t *testing.T) {
		ctx := t.Context()
		prefix := fmt.Sprintf("keyset-%d-", time.Now().UnixNano())

		var want []values.DomainName
		for _, suffix := range []string{"c", "a", "b"} {
			name := values.MustParse(prefix + suffix + ".test")
			d, err := New(name)
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, d))
			want = append(want, name)
		}
		slices.SortFunc(want, func(a, b values.DomainName) int { return strings.Compare(a.String(), b.String()) })

		cursor := values.MustParse(prefix + ".test")
		first, err := repo.ListPage(ctx, keyset.Page[values.DomainName]{After: &cursor, Limit: 2})
		require.NoError(t, err)
		require.Len(t, first, 2)
		assert.Equal(t, want[0], first[0].Name())
		assert.Equal(t, want[1], first[1].Name())

		after := first[1].Name()
		second, err := repo.ListPage(ctx, keyset.Page[values.DomainName]{After: &after, Limit: 2})
		require.NoError(t, err)
		require.NotEmpty(t, second)
		assert.Equal(t, want[2], second[0].Name(), "the cursor itself must not be returned again")
	})
}
