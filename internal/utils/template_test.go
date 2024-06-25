package utils_test

import (
	"testing"

	"github.com/ludusrusso/kannon/internal/utils"
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
