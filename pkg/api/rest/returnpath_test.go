package rest_test

import (
	"strings"

	"github.com/kannon-email/kannon/internal/returnpath"
)

var testReturnPath = returnpath.MustParse(strings.Repeat("s", returnpath.MinSecretLength))
