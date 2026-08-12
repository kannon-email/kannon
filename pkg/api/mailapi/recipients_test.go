package mailapi

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecipientsFromRequestKeepsAFailurePerRecipient pins the property the whole
// shape exists for: translating the list is total. A row this build cannot read
// yields a row carrying the error, never an error over the list, because the caller
// of an unreadable row is owed a Rejected row and a Batch of the others (#419).
func TestRecipientsFromRequestKeepsAFailurePerRecipient(t *testing.T) {
	got := recipientsFromRequest([]*mailertypes.Recipient{
		{Email: "first@email.com", Fields: map[string]string{"name": "First"}},
		{Email: "unreadable@email.com", Tracking: &trackingtypes.TrackingPolicy{
			Opens: trackingtypes.TrackingMode(9999),
		}},
		{Email: "last@email.com", Tracking: &trackingtypes.TrackingPolicy{
			Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
		}},
	})

	require.Len(t, got, 3, "one domain Recipient per stated row, in the order stated")

	assert.Equal(t, "first@email.com", got[0].Email)
	assert.Equal(t, map[string]string{"name": "First"}, got[0].Fields)
	assert.Equal(t, tracking.Policy{}, got[0].Tracking, "an omitted Policy states nothing")
	assert.NoError(t, got[0].trackingErr)

	assert.Equal(t, "unreadable@email.com", got[1].Email)
	assert.Error(t, got[1].trackingErr, "the unreadable Mode must travel on its own row")

	assert.Equal(t, "last@email.com", got[2].Email)
	assert.Equal(t, tracking.Policy{Opens: tracking.ModeOff}, got[2].Tracking,
		"a row after an unreadable one is translated normally")
	assert.NoError(t, got[2].trackingErr)
}

// TestRecipientsFromRequestSurvivesANilRow covers the one shape only an in-process
// caller can produce: the wire never decodes a nil element into a repeated field.
// It is an empty row of somebody's list, and is worth no more than the address it
// does not have.
func TestRecipientsFromRequestSurvivesANilRow(t *testing.T) {
	got := recipientsFromRequest([]*mailertypes.Recipient{nil})

	require.Len(t, got, 1)
	assert.False(t, got[0].HasAddress())
	assert.NoError(t, got[0].trackingErr)
}
