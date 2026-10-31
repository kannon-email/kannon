package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/stretchr/testify/assert"
)

func TestErrorKeepsIdentityAndCause(t *testing.T) {
	sentinel := apperr.New(apperr.NotFound, "not_found", "widget not found")
	wrapped := fmt.Errorf("load: %w", sentinel)

	assert.ErrorIs(t, wrapped, sentinel)
	got, ok := apperr.As(wrapped)
	assert.True(t, ok)
	assert.Equal(t, apperr.NotFound, got.Kind)
	assert.Equal(t, "not_found", got.Code)
	assert.Equal(t, "widget not found", sentinel.Error())

	cause := errors.New("subject contains forbidden CR/LF")
	invalid := apperr.Wrap(apperr.Invalid, "validation_failed", cause)
	assert.ErrorIs(t, invalid, cause)
	assert.Equal(t, cause.Error(), invalid.Error())

	assert.Equal(t, "too_many", apperr.New(apperr.Invalid, "too_many", "").Error())

	got, ok = apperr.As(errors.New("database is down"))
	assert.False(t, ok)
	assert.Nil(t, got)
}
