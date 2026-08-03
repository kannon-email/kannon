package sqlc

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

func TestDomainsRepository(t *testing.T) {
	t.Cleanup(func() {
		//nolint:errcheck // best-effort test cleanup
		db.Exec(context.Background(), "DELETE FROM domains CASCADE")
	})
	_, err := db.Exec(t.Context(), "DELETE FROM domains CASCADE")
	require.NoError(t, err)

	repo := NewDomainsRepository(db)
	domains.RunRepoSpec(t, repo)
}

// TestDomainCeilingSurvivesAMalformedRow covers the one case the repository spec cannot: a
// `tracking` value that never went through this repository's write path. A ceiling that states
// nothing enforces nothing (ADR 0003), so a row edited by hand must enforce the floor instead.
func TestDomainCeilingSurvivesAMalformedRow(t *testing.T) {
	t.Cleanup(func() {
		//nolint:errcheck // best-effort test cleanup
		db.Exec(context.Background(), "DELETE FROM domains CASCADE")
	})

	repo := NewDomainsRepository(db)
	name := values.MustParse("malformed-tracking.test")

	d, err := domains.New(name)
	require.NoError(t, err)
	require.NoError(t, repo.Create(t.Context(), d))

	// Straight SQL, as an operator or a hand-written migration would: links is
	// left unstated entirely.
	_, err = db.Exec(t.Context(),
		`UPDATE domains SET tracking = '{"opens":"identified"}'::jsonb WHERE domain = $1`, name.String())
	require.NoError(t, err)

	got, err := repo.FindByName(t.Context(), name)
	require.NoError(t, err)
	require.Equal(t, tracking.ModeIdentified, got.TrackingPolicy().Opens)
	require.Equal(t, tracking.ModeOff, got.TrackingPolicy().Links,
		"an axis a Domain leaves unstated must enforce the floor, not nothing")
}
