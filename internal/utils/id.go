package utils

import (
	"fmt"

	"github.com/nrednav/cuid2"
)

func NewID(prefix string) string {
	return fmt.Sprintf("%v_%v", prefix, cuid2.Generate())
}
