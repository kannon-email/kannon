package domains

import (
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunRepoSpec exercises any Repository implementation against the documented
// behaviour. Implementations must pass every sub-test.
func RunRepoSpec(t *testing.T, repo Repository) {
	t.Run("Create", func(t *testing.T) { testCreate(t, repo) })
	t.Run("FindByName", func(t *testing.T) { testFindByName(t, repo) })
	t.Run("List", func(t *testing.T) { testList(t, repo) })
	t.Run("SetTrackingPolicy", func(t *testing.T) { testSetTrackingPolicy(t, repo) })
}

func freshFQDN(prefix string) string {
	return fmt.Sprintf("%s-%d.test", prefix, time.Now().UnixNano())
}

func testCreate(t *testing.T, repo Repository) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		fqdn := freshFQDN("create")

		d, err := New(fqdn)
		require.NoError(t, err)
		assert.NotEmpty(t, d.DkimPrivateKey(), "New should generate a DKIM private key")
		assert.NotEmpty(t, d.DkimPublicKey(), "New should generate a DKIM public key")

		require.NoError(t, repo.Create(ctx, d))
		assert.NotZero(t, d.ID(), "ID should be populated after create")
		assert.False(t, d.CreatedAt().IsZero(), "CreatedAt should be populated after create")

		fetched, err := repo.FindByName(ctx, fqdn)
		require.NoError(t, err)
		assert.Equal(t, d.ID(), fetched.ID())
		assert.Equal(t, fqdn, fetched.Domain())
		assert.Equal(t, d.DkimPublicKey(), fetched.DkimPublicKey())
		assert.Equal(t, d.DkimPrivateKey(), fetched.DkimPrivateKey())
	})

	t.Run("EmptyFQDN", func(t *testing.T) {
		_, err := New("")
		assert.Error(t, err)
	})
}

func testFindByName(t *testing.T, repo Repository) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		fqdn := freshFQDN("find")

		d, err := New(fqdn)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		fetched, err := repo.FindByName(ctx, fqdn)
		require.NoError(t, err)
		assert.Equal(t, fqdn, fetched.Domain())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.FindByName(ctx, freshFQDN("missing"))
		assert.ErrorIs(t, err, ErrDomainNotFound)
	})
}

// testSetTrackingPolicy asserts the one invariant that is only observable at
// the repository boundary: a Mode that states nothing never survives at rest on
// a Domain, because the Domain is the ceiling and a ceiling that states nothing
// would be unenforceable.
func testSetTrackingPolicy(t *testing.T, repo Repository) {
	t.Run("UnstatedModeIsNormalisedToOff", func(t *testing.T) {
		ctx := t.Context()
		fqdn := freshFQDN("tracking")

		d, err := New(fqdn)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, d))

		// Only opens is stated; links states nothing.
		updated, err := repo.SetTrackingPolicy(ctx, fqdn, tracking.Policy{Opens: tracking.ModeAnonymous})
		require.NoError(t, err)

		want := tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff}
		assert.Equal(t, want, updated.TrackingPolicy())

		fetched, err := repo.FindByName(ctx, fqdn)
		require.NoError(t, err)
		assert.Equal(t, want, fetched.TrackingPolicy(), "an unstated Mode must not survive at rest")
	})
}

func testList(t *testing.T, repo Repository) {
	t.Run("ContainsCreatedDomains", func(t *testing.T) {
		ctx := t.Context()

		want := map[string]bool{}
		for i := range 3 {
			fqdn := freshFQDN(fmt.Sprintf("list-%d", i))
			d, err := New(fqdn)
			require.NoError(t, err)
			require.NoError(t, repo.Create(ctx, d))
			want[fqdn] = true
		}

		all, err := repo.List(ctx)
		require.NoError(t, err)

		got := map[string]bool{}
		for _, d := range all {
			got[d.Domain()] = true
		}
		for fqdn := range want {
			assert.True(t, got[fqdn], "List should include %q", fqdn)
		}
	})
}
