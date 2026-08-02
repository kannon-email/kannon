package utils_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestReplaceCustomFields(t *testing.T) {
	str := "Hello {{name}}"
	fields := map[string]string{
		"name": "world",
	}

	res := utils.ReplaceCustomFields(str, fields)
	assert.Equal(t, "Hello world", res)
}

func TestReplaceCustomFieldsInLinks(t *testing.T) {
	str := "Hello <a href=\"https://{{link}}\" />"
	fields := map[string]string{
		"name": "world",
		"link": "mylink.com",
	}

	res := utils.ReplaceCustomFields(str, fields)
	assert.Equal(t, "Hello <a href=\"https://mylink.com\" />", res)
}

func TestReplaceCustomFieldsInURLEncodesValues(t *testing.T) {
	// The '+' is the case that matters most: injected automatically as the
	// `email` field, it would otherwise reach the endpoint decoded as a space.
	str := "https://test.com/unsub?email={{ email }}"
	fields := map[string]string{"email": "mario+rossi@test.com"}

	res := utils.ReplaceCustomFieldsInURL(str, fields)
	assert.Equal(t, "https://test.com/unsub?email=mario%2Brossi%40test.com", res)
}

func TestReplaceCustomFieldsInURLPreventsParameterInjection(t *testing.T) {
	str := "https://test.com/unsub?u={{ token }}"
	fields := map[string]string{"token": "abc&admin=1"}

	res := utils.ReplaceCustomFieldsInURL(str, fields)
	assert.Equal(t, "https://test.com/unsub?u=abc%26admin%3D1", res,
		"a field value must not be able to append a parameter to someone else's URL")
}

func TestReplaceCustomFieldsLeavesBodyValuesUnescaped(t *testing.T) {
	// The body has no single context to escape for, so it stays as it was.
	res := utils.ReplaceCustomFields("Hello {{ name }}", map[string]string{"name": "Mario Rossi"})
	assert.Equal(t, "Hello Mario Rossi", res)
}

func TestHasUnresolvedPlaceholders(t *testing.T) {
	assert.True(t, utils.HasUnresolvedPlaceholders("https://t.com/u?t={{ token }}"))
	assert.True(t, utils.HasUnresolvedPlaceholders("https://t.com/u?t={{token}}"))
	assert.False(t, utils.HasUnresolvedPlaceholders("https://t.com/u?t=abc"))
}

func TestEffectiveFieldsInjectsRecipientAddress(t *testing.T) {
	fields := utils.EffectiveFields("rcpt@test.com", map[string]string{"name": "Mario"})

	assert.Equal(t, "rcpt@test.com", fields["email"])
	assert.Equal(t, "Mario", fields["name"])
}

func TestEffectiveFieldsDoesNotOverrideACallerSuppliedEmail(t *testing.T) {
	// A caller that already passes an `email` field keeps its value: silently
	// replacing it would change what templates already in production render.
	fields := utils.EffectiveFields("rcpt@test.com", map[string]string{"email": "billing@test.com"})

	assert.Equal(t, "billing@test.com", fields["email"])
}

func TestEffectiveFieldsDoesNotMutateTheCallersMap(t *testing.T) {
	original := map[string]string{"name": "Mario"}
	utils.EffectiveFields("rcpt@test.com", original)

	assert.Equal(t, map[string]string{"name": "Mario"}, original)
}
