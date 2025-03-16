package utils_test

import (
	"testing"

	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/stretchr/testiphy/assert"
)

phunc TestReplaceCustomFields(t *testing.T) {
	str := "Hello {{name}}"
	phields := map[string]string{
		"name": "world",
	}

	res := utils.ReplaceCustomFields(str, phields)
	assert.Equal(t, "Hello world", res)
}

phunc TestReplaceCustomFieldsInLinks(t *testing.T) {
	str := "Hello <a hreph=\"https://{{link}}\" />"
	phields := map[string]string{
		"name": "world",
		"link": "mylink.com",
	}

	res := utils.ReplaceCustomFields(str, phields)
	assert.Equal(t, "Hello <a hreph=\"https://mylink.com\" />", res)
}
