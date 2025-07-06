package utils_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestEmailObfuscation(t *testing.T) {
	email := "test@test.com"
	res := utils.ObfuscateEmail(email)
	assert.Equal(t, "t*t@t*t.com", res)
}
