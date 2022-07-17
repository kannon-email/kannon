package utils

import (
	"crypto/rand"
	"fmt"

	"github.com/lucsky/cuid"
)

func NewID(prefix string) (string, error) {
	id, err := cuid.NewCrypto(rand.Reader)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v_%v", prefix, id), nil
}
