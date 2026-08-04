package authz_test

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a claim may say. Nothing here is a check on the person — there is nobody to check against,
// which is what makes an Attribution unverifiable in principle (ADR 0008). These are checks on what
// a record can hold: a claim is written down, and the shape of it is settled at the boundary it
// arrived at rather than by whatever it breaks further in.
func TestParseAttribution(t *testing.T) {
	tests := []struct {
		name  string
		given string
		want  authz.Attribution
	}{
		{
			name:  "an address, which is what a front-end has to name somebody by",
			given: "alice@corp.com",
			want:  "alice@corp.com",
		},
		{
			// Kannon never parses the claim, so an opaque identifier is as
			// good as an address and neither is preferred here.
			name:  "an opaque identifier from the calling system",
			given: "user_01HQ8Z3P",
			want:  "user_01HQ8Z3P",
		},
		{
			name:  "a name outside ASCII, since people have them",
			given: "Zoë Müller <zoe@corp.com>",
			want:  "Zoë Müller <zoe@corp.com>",
		},
		{
			// Padding is what a value copied through a config field or a header
			// arrives with; keeping it would record two spellings of one person.
			name:  "padding, which is not part of the name",
			given: "  alice@corp.com \n",
			want:  "alice@corp.com",
		},
		{
			name:  "exactly the limit",
			given: strings.Repeat("a", 256),
			want:  authz.Attribution(strings.Repeat("a", 256)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := authz.ParseAttribution(tc.given)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseAttributionRefusals(t *testing.T) {
	tests := []struct {
		name  string
		given string
	}{
		{
			// A caller with nobody to name sends no header. An empty claim is
			// therefore a bug in the caller, not a request to record nobody.
			name:  "nothing at all",
			given: "",
		},
		{
			name:  "whitespace, which names nobody either",
			given: "   \t ",
		},
		{
			name:  "one byte over the limit",
			given: strings.Repeat("a", 257),
		},
		{
			// Refused here rather than at the far end of the request: a header
			// carries arbitrary bytes and a text column will not take these.
			name:  "bytes that are not UTF-8",
			given: "alice\xff@corp.com",
		},
		{
			name:  "a control character, which forges the structure of a record",
			given: "alice@corp.com\x00admin",
		},
		{
			// Trimming takes one at either end, so this one is in the middle: a claim
			// spanning two lines writes a second line into the record.
			name:  "a line break, for the same reason",
			given: "alice@corp.com\nlevel=INFO msg=\"attributed operation\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := authz.ParseAttribution(tc.given)

			assert.Error(t, err, "expected %q to be refused", tc.given)
			assert.Empty(t, got, "a refusal must not hand back a claim")
		})
	}
}
