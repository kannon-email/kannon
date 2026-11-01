package statssec

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunKeyRepoSpec exercises any KeyRepository implementation against the documented behaviour.
// Every sub-test tolerates keys other sub-tests left behind, since an implementation may be backed
// by a database shared with them.
func RunKeyRepoSpec(t *testing.T, repo KeyRepository) {
	t.Run("CreateThenPublicKey", func(t *testing.T) {
		ctx := t.Context()
		k := freshKey(time.Hour)
		require.NoError(t, repo.Create(ctx, k))

		public, err := repo.PublicKey(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, k.PublicKey, public)
	})

	t.Run("DuplicateIDIsRefused", func(t *testing.T) {
		ctx := t.Context()
		k := freshKey(time.Hour)
		require.NoError(t, repo.Create(ctx, k))

		other := k
		other.PublicKey = "another-public-key"
		require.Error(t, repo.Create(ctx, other))

		public, err := repo.PublicKey(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, k.PublicKey, public, "the first key must survive a refused duplicate")
	})

	t.Run("PublicKeyOfAnUnknownKey", func(t *testing.T) {
		_, err := repo.PublicKey(t.Context(), freshKey(time.Hour).ID)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("PublicKeyOfAnExpiredKey", func(t *testing.T) {
		ctx := t.Context()
		k := freshKey(-time.Hour)
		require.NoError(t, repo.Create(ctx, k))

		_, err := repo.PublicKey(ctx, k.ID)
		assert.ErrorIs(t, err, ErrKeyNotFound, "an expired key must not verify anything")
	})

	t.Run("SigningKeyOutlivesValidUntil", func(t *testing.T) {
		ctx := t.Context()
		require.NoError(t, repo.Create(ctx, freshKey(3*time.Hour)))

		validUntil := time.Now().Add(2 * time.Hour)
		k, err := repo.SigningKey(ctx, validUntil)
		require.NoError(t, err)
		assert.True(t, k.ExpiresAt.After(validUntil), "a signing key must outlive the token it signs")
		assert.NotEmpty(t, k.PrivateKey)
		assert.NotEmpty(t, k.PublicKey)
	})

	t.Run("NoSigningKeyLivesLongEnough", func(t *testing.T) {
		ctx := t.Context()
		require.NoError(t, repo.Create(ctx, freshKey(time.Hour)))

		_, err := repo.SigningKey(ctx, time.Now().AddDate(100, 0, 0))
		assert.ErrorIs(t, err, ErrNoSigningKey)
	})

	t.Run("DeleteExpiredSparesValidKeys", func(t *testing.T) {
		ctx := t.Context()
		expired, valid := freshKey(-time.Hour), freshKey(time.Hour)
		require.NoError(t, repo.Create(ctx, expired))
		require.NoError(t, repo.Create(ctx, valid))

		deleted, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.Positive(t, deleted)

		again, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.Zero(t, again, "every expired key went in the first sweep")

		public, err := repo.PublicKey(ctx, valid.ID)
		require.NoError(t, err)
		assert.Equal(t, valid.PublicKey, public)
	})
}

// freshKey returns a key with an ID no other run uses, expiring ttl from now. The PEM fields are
// opaque to a KeyRepository, so they need not be real keys.
func freshKey(ttl time.Duration) Key {
	n := time.Now().UnixNano()
	return Key{
		ID:         fmt.Sprintf("key_spec_%d", n),
		PrivateKey: fmt.Sprintf("private-%d", n),
		PublicKey:  fmt.Sprintf("public-%d", n),
		ExpiresAt:  time.Now().Add(ttl),
	}
}
