package verifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidEmail(t *testing.T) {
	err := verifyEmail("test@test.com")
	assert.Nil(t, err)
}

func TestInvalidEmail(t *testing.T) {
	err := verifyEmail("thisisnota validemail-test.com")
	assert.NotNil(t, err)
	assert.ErrorIs(t, ErrInvalidEmailAddress, err)
}
