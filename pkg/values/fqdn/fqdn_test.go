package fqdn

import "testing"

func TestValidateFQDN(t *testing.T) {
	tt := []struct {
		fqdn        string
		expectedErr error
	}{
		{
			fqdn:        "test",
			expectedErr: ErrInvalidFQDN,
		},
		{
			fqdn:        "test.com",
			expectedErr: nil,
		},
		{
			fqdn:        "test.com.",
			expectedErr: ErrInvalidFQDN,
		},
		{
			fqdn:        "exp.test.com",
			expectedErr: nil,
		},
		{
			fqdn:        "exp.test.com.",
			expectedErr: ErrInvalidFQDN,
		},
	}

	for _, tc := range tt {
		t.Run(tc.fqdn, func(t *testing.T) {
			_, err := NewFQDN(tc.fqdn)
			if tc.expectedErr != nil {
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
