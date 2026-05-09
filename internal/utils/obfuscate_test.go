package utils_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestEmailObfuscation(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{"short local", "test@test.com", "t***@test.com"},
		{"single char local", "a@test.com", "a@test.com"},
		{"long local", "ludovico.russo@example.org", "l*************@example.org"},
		{"subdomain preserved", "john.doe@mail.example.co.uk", "j*******@mail.example.co.uk"},
		{"plus tag in local", "user+tag@gmail.com", "u*******@gmail.com"},
		{"uppercase preserved", "Alice@Example.COM", "A****@Example.COM"},
		{"missing at sign", "not-an-email", "not-an-email"},
		{"empty local part", "@test.com", "@test.com"},
		{"empty string", "", ""},
		{"multiple at signs uses last", "weird@name@host.com", "w*********@host.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, utils.ObfuscateEmail(tc.email))
		})
	}
}
