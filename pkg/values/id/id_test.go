package id

import (
	"strings"
	"testing"

	"github.com/ludusrusso/kannon/pkg/values/fqdn"
	"github.com/stretchr/testify/assert"
)

func TestCreateID(t *testing.T) {
	id, err := CreateID("test")
	if err != nil {
		t.Errorf("CreateID() error = %v, want nil", err)
	}

	assert.NotEmpty(t, id)
}

func TestInvalidID(t *testing.T) {
	id := ID("")

	t.Run("ID Should Be empty", func(t *testing.T) {
		assert.True(t, id.IsEmpty())
	})

	t.Run("ID Should Not Be Valid", func(t *testing.T) {
		err := id.Validate()
		assert.Error(t, err)
		assert.Equal(t, ErrEmptyID, err)
	})
}

func TestValidID(t *testing.T) {
	id := ID("test")

	t.Run("ID Should Be empty", func(t *testing.T) {
		assert.False(t, id.IsEmpty())
	})

	t.Run("ID Should Not Be Valid", func(t *testing.T) {
		err := id.Validate()
		assert.NoError(t, err)
	})
}

func TestCreateScopedID(t *testing.T) {
	id, err := CreateScopedID("test", fqdn.FQDN("example.com"))
	if err != nil {
		t.Errorf("CreateScopedID() error = %v, want nil", err)
	}

	assert.True(t, strings.HasPrefix(id.String(), "test_"), "should start with test_")
	assert.True(t, strings.HasSuffix(id.String(), "@example.com"), "should end with @example.com")
}
