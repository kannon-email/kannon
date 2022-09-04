package validator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidEmail(t *testing.T) {
	err := validateEmail("test@test.com")
	assert.Nil(t, err)
}

func TestInvalidEmail(t *testing.T) {
	err := validateEmail("thisisnota validemail-test.com")
	assert.NotNil(t, err)
	assert.ErrorIs(t, ErrInvalidEmailAddress, err)
}
