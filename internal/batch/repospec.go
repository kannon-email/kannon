package batch

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSenderAlias is the Sender alias shared by the repo spec's fixture
// Batches; factored out so goconst does not flag its repeated use.
const testSenderAlias = "From"

// RepoTestHelper provides test utilities for repository spec tests.
type RepoTestHelper interface {
	// CreateDomain creates a fresh domain row, registers cleanup, and
	// returns the domain name. CreateMessage requires an existing domain
	// and an existing template, so the helper is expected to also seed a
	// template scoped to that domain (callers create their own batches).
	CreateDomain(t *testing.T) string

	// CreateTemplate creates a template under the given domain and returns
	// its template_id, suitable for use as Batch.TemplateID().
	CreateTemplate(t *testing.T, domain string) string
}

// RunRepoSpec exercises any Repository implementation against the documented
// behaviour. Implementations must pass every sub-test.
func RunRepoSpec(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Create", func(t *testing.T) {
		testCreate(t, repo, helper)
	})
	t.Run("GetByID", func(t *testing.T) {
		testGetByID(t, repo, helper)
	})
	t.Run("TrackingPolicyProvenance", func(t *testing.T) {
		testTrackingPolicyProvenance(t, repo, helper)
	})
}

func testCreate(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)
		tpl := helper.CreateTemplate(t, domain)

		b, err := New(domain, "hello", Sender{Email: "from@" + domain, Alias: testSenderAlias}, tpl, nil, Headers{}, tracking.Policy{})
		require.NoError(t, err)

		err = repo.Create(ctx, b)
		require.NoError(t, err)
	})

	t.Run("WithAttachmentsAndHeaders", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)
		tpl := helper.CreateTemplate(t, domain)

		atts := Attachments{"a.txt": []byte("hi")}
		hdrs := Headers{To: []string{"to@" + domain}, Cc: []string{"cc@" + domain}}
		b, err := New(domain, "hello", Sender{Email: "from@" + domain, Alias: testSenderAlias}, tpl, atts, hdrs, tracking.Policy{})
		require.NoError(t, err)

		err = repo.Create(ctx, b)
		require.NoError(t, err)

		fetched, err := repo.GetByID(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, []byte("hi"), fetched.Attachments()["a.txt"])
		assert.Equal(t, []string{"to@" + domain}, fetched.Headers().To)
		assert.Equal(t, []string{"cc@" + domain}, fetched.Headers().Cc)
	})
}

func testGetByID(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)
		tpl := helper.CreateTemplate(t, domain)

		b, err := New(domain, "hello", Sender{Email: "from@" + domain, Alias: "Alias"}, tpl, nil, Headers{}, tracking.Policy{})
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, b))

		fetched, err := repo.GetByID(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, b.ID(), fetched.ID())
		assert.Equal(t, b.Subject(), fetched.Subject())
		assert.Equal(t, b.Sender().Email, fetched.Sender().Email)
		assert.Equal(t, b.Sender().Alias, fetched.Sender().Alias)
		assert.Equal(t, b.TemplateID(), fetched.TemplateID())
		assert.Equal(t, b.Domain(), fetched.Domain())
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.GetByID(ctx, "msg_nonexistent@nowhere.test")
		assert.ErrorIs(t, err, ErrBatchNotFound)
	})
}

// testTrackingPolicyProvenance asserts the one invariant that is only
// observable at the repository boundary: the Batch column is provenance, not
// the resolved value, so it must round-trip exactly as the caller stated it —
// including a Mode that states nothing, which is never normalised away here
// (ADR 0003: "messages.tracking is the only place an unstated Mode can be
// stored").
func testTrackingPolicyProvenance(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("StatedPolicyRoundTrips", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)
		tpl := helper.CreateTemplate(t, domain)

		stated := tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeOff}
		b, err := New(domain, "hello", Sender{Email: "from@" + domain, Alias: testSenderAlias}, tpl, nil, Headers{}, stated)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, b))

		assert.Equal(t, stated, b.TrackingPolicy(), "the in-memory Batch must keep exactly what was stated")

		fetched, err := repo.GetByID(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, stated, fetched.TrackingPolicy())
	})

	t.Run("UnstatedModeSurvivesUnnormalised", func(t *testing.T) {
		ctx := t.Context()
		domain := helper.CreateDomain(t)
		tpl := helper.CreateTemplate(t, domain)

		// Only opens is stated; links states nothing. Unlike the Domain
		// ceiling, the Batch provenance column must not normalise this away.
		stated := tracking.Policy{Opens: tracking.ModeAnonymous}
		b, err := New(domain, "hello", Sender{Email: "from@" + domain, Alias: testSenderAlias}, tpl, nil, Headers{}, stated)
		require.NoError(t, err)
		require.NoError(t, repo.Create(ctx, b))

		fetched, err := repo.GetByID(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, stated, fetched.TrackingPolicy(), "an unstated Mode must survive unnormalised as provenance")
		assert.Equal(t, tracking.ModeUnspecified, fetched.TrackingPolicy().Links, "links must remain unstated, not normalised to off")
	})
}
