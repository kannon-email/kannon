package utils_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestExtractMessageID(t *testing.T) {
	msg := "<bHVkdXMucnVzc29AZ21haWwuY29t/msg_cl5f3gh800000k684hvyccv8w@test.dev>"
	expected := "msg_cl5f3gh800000k684hvyccv8w@test.dev"
	id, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(msg)
	assert.Equal(t, expected, id)
	assert.Equal(t, "test.dev", domain)
	assert.Nil(t, err)
}

func TestExtractMsgIDAndDomainErr(t *testing.T) {
	msg := "msg_cl5f3gh800000k684hvyccv7w@test.dev"
	_, _, err := utils.ExtractMsgIDAndDomainFromEmailID(msg)
	assert.NotNil(t, err)
}

func TestParseBounceReturnPathNoBounce(t *testing.T) {
	returnPath := "xxx@test.com"
	//nolint:dogsled
	_, _, _, found, err := utils.ParseBounceReturnPath(returnPath)
	assert.Nil(t, err)
	assert.False(t, found)
}

func TestParseBounceReturnPath(t *testing.T) {
	returnPath := "bump_dGVzdEB0ZXN0LmNvbQ==+msg_cl6g7ndft0001018ut5octeun@k.test.com"
	email, messageID, domain, found, err := utils.ParseBounceReturnPath(returnPath)
	assert.Nil(t, err)
	assert.True(t, found)
	assert.Equal(t, "k.test.com", domain)
	assert.Equal(t, "test@test.com", email)
	assert.Equal(t, "msg_cl6g7ndft0001018ut5octeun@k.test.com", messageID)
}

// buildReturnPathForTest replicates buildReturnPath from internal/envelope/message.go,
// which is unexported and therefore not reachable from here. This copy could in
// principle drift from the real encoder, so the encoder and decoder are also pinned
// together by TestBuildReturnPathRoundTripsThroughParseBounceReturnPath in
// internal/envelope, which calls the real buildReturnPath. What this test adds is
// the decoder's own contract: it must accept the URL-safe alphabet.
func buildReturnPathForTest(to, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("bump_%v+%v", emailBase64, messageID)
}

// TestParseBounceReturnPathRoundTripsURLSafeAlphabet round-trips buildReturnPath's
// output through ParseBounceReturnPath for addresses whose base64 encoding is known
// to hit the two symbols where the URL-safe and standard alphabets diverge ('-'/'_'
// vs '+'/'/'). Before this fix, ParseBounceReturnPath decoded with the standard
// alphabet while buildReturnPath encoded with the URL-safe one, so a return path
// containing '-' or '_' failed to decode and its bounce was silently dropped (#432).
func TestParseBounceReturnPathRoundTripsURLSafeAlphabet(t *testing.T) {
	tests := []struct {
		name string
		// email is a syntactically valid address chosen so its base64 encoding
		// lands exactly on a divergent alphabet symbol; see the assertion below,
		// which checks this rather than assuming it.
		email      string
		divergesOn string // the URL-safe-only symbol this fixture's encoding must contain
	}{
		{
			name:       "local part with a trailing '~' forces the '-' symbol",
			email:      "ab~@test.com",
			divergesOn: "-",
		},
		{
			name:       "non-ASCII (SMTPUTF8-style) local part forces the '_' symbol",
			email:      "aÿ@test.com", // 'ÿ', U+00FF
			divergesOn: "_",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			messageID := "msg_cl6g7ndft0001018ut5octeun@k.test.com"
			returnPath := buildReturnPathForTest(tc.email, messageID)

			// Confirm the fixture actually exercises the divergent symbol instead
			// of assuming it: the URL-safe encoding must contain it, and the
			// standard encoding of the same address must differ because of it.
			stdEncoded := base64.StdEncoding.EncodeToString([]byte(tc.email))
			urlEncoded := base64.URLEncoding.EncodeToString([]byte(tc.email))
			assert.Contains(t, urlEncoded, tc.divergesOn, "fixture must produce the URL-safe symbol under test")
			assert.NotEqual(t, stdEncoded, urlEncoded, "fixture must make the two alphabets disagree")
			assert.True(t, strings.Contains(returnPath, tc.divergesOn))

			email, gotMessageID, domain, found, err := utils.ParseBounceReturnPath(returnPath)
			assert.Nil(t, err)
			assert.True(t, found)
			assert.Equal(t, tc.email, email)
			assert.Equal(t, messageID, gotMessageID)
			assert.Equal(t, "k.test.com", domain)
		})
	}
}
