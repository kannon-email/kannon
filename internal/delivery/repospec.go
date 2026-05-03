package delivery

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RepoTestHelper provides test utilities for repository spec tests.
type RepoTestHelper interface {
	// CreateBatch seeds the prerequisite domain + template + batch rows so
	// foreign-key constraints on sending_pool_emails are satisfied, and
	// returns the BatchID and Domain to use when constructing Deliveries.
	CreateBatch(t *testing.T) (batch.ID, string)
}

// RunRepoSpec exercises any Repository implementation against the documented
// behaviour. Implementations must pass every sub-test.
func RunRepoSpec(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Schedule", func(t *testing.T) {
		testSchedule(t, repo, helper)
	})
	t.Run("Get", func(t *testing.T) {
		testGet(t, repo, helper)
	})
	t.Run("PrepareForValidate", func(t *testing.T) {
		testPrepareForValidate(t, repo, helper)
	})
	t.Run("SetScheduled_PrepareForSend", func(t *testing.T) {
		testSetScheduledThenPrepareForSend(t, repo, helper)
	})
	t.Run("Reschedule", func(t *testing.T) {
		testReschedule(t, repo, helper)
	})
	t.Run("Clean", func(t *testing.T) {
		testClean(t, repo, helper)
	})
}

func newDelivery(t *testing.T, batchID batch.ID, domain, email string) *Delivery {
	t.Helper()
	d, err := New(NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        map[string]string{"name": "X"},
		Domain:        domain,
		ScheduledTime: time.Now().UTC().Add(-time.Minute),
		Backoff:       DefaultBackoff,
	})
	require.NoError(t, err)
	return d
}

func testSchedule(t *testing.T, repo Repository, helper RepoTestHelper) {
	ctx := t.Context()
	batchID, domain := helper.CreateBatch(t)
	d := newDelivery(t, batchID, domain, "to@"+domain)
	require.NoError(t, repo.Schedule(ctx, d))
}

func testGet(t *testing.T, repo Repository, helper RepoTestHelper) {
	t.Run("Success", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		email := "to@" + domain
		d := newDelivery(t, batchID, domain, email)
		require.NoError(t, repo.Schedule(ctx, d))

		got, err := repo.Get(ctx, batchID, email)
		require.NoError(t, err)
		assert.Equal(t, batchID, got.BatchID())
		assert.Equal(t, email, got.Email())
		assert.Equal(t, domain, got.Domain())
		assert.Equal(t, 0, got.SendAttempts())
		assert.Equal(t, "X", got.Fields()["name"])
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		_, err := repo.Get(ctx, batchID, "missing@"+domain)
		assert.ErrorIs(t, err, ErrDeliveryNotFound)
	})
}

func testPrepareForValidate(t *testing.T, repo Repository, helper RepoTestHelper) {
	ctx := t.Context()
	batchID, domain := helper.CreateBatch(t)
	email := "v@" + domain
	require.NoError(t, repo.Schedule(ctx, newDelivery(t, batchID, domain, email)))

	got, err := repo.PrepareForValidate(ctx, 10)
	require.NoError(t, err)

	found := false
	for _, d := range got {
		if d.BatchID() == batchID && d.Email() == email {
			found = true
		}
	}
	assert.True(t, found, "expected freshly scheduled delivery to be returned by PrepareForValidate")
}

func testSetScheduledThenPrepareForSend(t *testing.T, repo Repository, helper RepoTestHelper) {
	ctx := t.Context()
	batchID, domain := helper.CreateBatch(t)
	email := "s@" + domain
	require.NoError(t, repo.Schedule(ctx, newDelivery(t, batchID, domain, email)))
	require.NoError(t, repo.SetScheduled(ctx, batchID, email))

	got, err := repo.PrepareForSend(ctx, 10)
	require.NoError(t, err)

	found := false
	for _, d := range got {
		if d.BatchID() == batchID && d.Email() == email {
			found = true
		}
	}
	assert.True(t, found, "expected scheduled delivery to be returned by PrepareForSend")
}

func testReschedule(t *testing.T, repo Repository, helper RepoTestHelper) {
	ctx := t.Context()
	batchID, domain := helper.CreateBatch(t)
	email := "r@" + domain
	require.NoError(t, repo.Schedule(ctx, newDelivery(t, batchID, domain, email)))
	require.NoError(t, repo.SetScheduled(ctx, batchID, email))

	require.NoError(t, repo.Reschedule(ctx, batchID, email))

	got, err := repo.Get(ctx, batchID, email)
	require.NoError(t, err)
	assert.Equal(t, 1, got.SendAttempts())
	// scheduledTime advanced by at least the floor (5min) past originalScheduledTime
	assert.True(t, got.ScheduledTime().After(got.OriginalScheduledTime()),
		"scheduled time should advance after reschedule")
}

func testClean(t *testing.T, repo Repository, helper RepoTestHelper) {
	ctx := t.Context()
	batchID, domain := helper.CreateBatch(t)
	email := "c@" + domain
	require.NoError(t, repo.Schedule(ctx, newDelivery(t, batchID, domain, email)))

	require.NoError(t, repo.Clean(ctx, batchID, email))

	_, err := repo.Get(ctx, batchID, email)
	assert.ErrorIs(t, err, ErrDeliveryNotFound)
}
