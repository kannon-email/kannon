package pool

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ClaimerTestHelper seeds prerequisite rows for Claimer specification
// tests and provides a way to schedule a fresh Delivery (since the
// Claimer itself does not own initial scheduling).
type ClaimerTestHelper interface {
	// CreateBatch seeds the prerequisite domain + template + batch rows
	// so foreign-key constraints on sending_pool_emails are satisfied,
	// and returns the BatchID and Domain.
	CreateBatch(t *testing.T) (batch.ID, string)

	// Schedule seeds a fresh Delivery in the pool. The Claimer does not
	// own initial scheduling — the harness must persist it directly.
	Schedule(t *testing.T, d *delivery.Delivery)
}

// RunClaimerSpec exercises any Claimer implementation against the
// documented behaviour.
func RunClaimerSpec(t *testing.T, c Claimer, helper ClaimerTestHelper) {
	t.Run("ClaimForValidation", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		email := "v@" + domain
		helper.Schedule(t, mustNewDelivery(t, batchID, domain, email))

		got, err := c.ClaimForValidation(ctx, 10)
		require.NoError(t, err)
		assert.True(t, containsKey(got, batchID, email),
			"expected freshly scheduled delivery to be returned by ClaimForValidation")
	})

	t.Run("MarkValidated_ClaimForDispatch", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		email := "s@" + domain
		dlv := mustNewDelivery(t, batchID, domain, email)
		helper.Schedule(t, dlv)

		require.NoError(t, c.MarkValidated(ctx, dlv))

		got, err := c.ClaimForDispatch(ctx, 10)
		require.NoError(t, err)
		assert.True(t, containsKey(got, batchID, email),
			"expected validated delivery to be returned by ClaimForDispatch")
	})

	t.Run("Reschedule", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		email := "r@" + domain
		dlv := mustNewDelivery(t, batchID, domain, email)
		helper.Schedule(t, dlv)
		require.NoError(t, c.MarkValidated(ctx, dlv))

		require.NoError(t, c.Reschedule(ctx, dlv))

		got, err := c.Lookup(ctx, batchID, email)
		require.NoError(t, err)
		assert.Equal(t, 1, got.SendAttempts())
		assert.True(t, got.ScheduledTime().After(got.OriginalScheduledTime()),
			"scheduled time should advance after reschedule")
	})

	t.Run("Drop", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		email := "d@" + domain
		dlv := mustNewDelivery(t, batchID, domain, email)
		helper.Schedule(t, dlv)

		require.NoError(t, c.Drop(ctx, dlv))

		_, err := c.Lookup(ctx, batchID, email)
		assert.ErrorIs(t, err, delivery.ErrDeliveryNotFound)
	})

	t.Run("Lookup_NotFound", func(t *testing.T) {
		ctx := t.Context()
		batchID, domain := helper.CreateBatch(t)
		_, err := c.Lookup(ctx, batchID, "missing@"+domain)
		assert.ErrorIs(t, err, delivery.ErrDeliveryNotFound)
	})
}

func mustNewDelivery(t *testing.T, batchID batch.ID, domain, email string) *delivery.Delivery {
	t.Helper()
	d, err := delivery.New(delivery.NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        map[string]string{"name": "X"},
		Domain:        domain,
		ScheduledTime: time.Now().UTC().Add(-time.Minute),
	})
	require.NoError(t, err)
	return d
}

func containsKey(ds []*delivery.Delivery, batchID batch.ID, email string) bool {
	for _, d := range ds {
		if d.BatchID() == batchID && d.Email() == email {
			return true
		}
	}
	return false
}
