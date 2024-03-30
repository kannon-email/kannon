package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailVaidation(t *testing.T) {
	tt := []struct {
		email     string
		expectErr bool
	}{
		{
			email:     "",
			expectErr: true,
		},
		{
			email:     "test",
			expectErr: true,
		},
		{
			email:     "test@",
			expectErr: true,
		},
		{
			email:     "test@test",
			expectErr: true,
		},
		{
			email:     "test@test.",
			expectErr: true,
		},
		{
			email:     "test@test.c",
			expectErr: true,
		},
		{
			email:     "test@emai.com",
			expectErr: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.email, func(t *testing.T) {
			_, err := NewEmail(tc.email)
			if tc.expectErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestUsername(t *testing.T) {
	email, err := NewEmail("test@test.com")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assert.Equal(t, email.Username(), "test")
	assert.Equal(t, email.Domain(), "test.com")
}
