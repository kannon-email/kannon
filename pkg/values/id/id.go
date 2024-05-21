package id

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/lucsky/cuid"
	"github.com/ludusrusso/kannon/pkg/values/fqdn"
)

type ID string

func (id ID) String() string {
	return string(id)
}

func (id ID) IsEmpty() bool {
	return id == ""
}

func (id ID) Validate() error {
	if id.IsEmpty() {
		return ErrEmptyID
	}
	return nil
}

var (
	ErrCannotCreateID = errors.New("cannot create id")
	ErrEmptyID        = errors.New("empty id")
)

func FromString(id string) ID {
	return ID(id)
}

func CreateID(prefix string) (ID, error) {
	id, err := cuid.NewCrypto(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCannotCreateID, err)
	}

	return ID(fmt.Sprintf("%s_%s", prefix, id)), nil
}

func CreateScopedID(prefix string, d fqdn.FQDN) (ID, error) {
	id, err := CreateID(prefix)
	if err != nil {
		return "", err
	}

	return ID(fmt.Sprintf("%s@%s", id, d)), nil
}

func NewID(id string) (ID, error) {
	i := ID(id)
	return i, i.Validate()
}
