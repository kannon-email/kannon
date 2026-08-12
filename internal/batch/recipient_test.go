package batch

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
)

func TestRecipientHasAddress(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  bool
	}{
		{name: "Address", email: "someone@example.com", want: true},
		{name: "Empty", email: "", want: false},
		{name: "Spaces", email: "   ", want: false},
		{name: "Tab", email: "\t", want: false},
		{name: "Newline", email: "\n", want: false},
		// Trimming decides whether there is an address at all, and nothing more:
		// what a padded address means is settled where the Delivery is built.
		{name: "PaddedAddress", email: "  someone@example.com  ", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Recipient{Email: tc.email, Tracking: tracking.Policy{Opens: tracking.ModeOff}}
			assert.Equal(t, tc.want, r.HasAddress())
		})
	}
}
