package cmd

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/x/container"
	"github.com/spf13/viper"
)

// A process asked to serve the API without a credential for it is refused at boot, and one serving
// only workers is not — the credential belongs on the hosts that answer admin requests and nowhere
// else. The refusal names both spellings of the key, since that message is all the operator gets.
func TestRequireAdminToken(t *testing.T) {
	tests := []struct {
		name    string
		flags   RunFlags
		token   string
		wantErr bool
	}{
		{
			name:    "the API is served with no token configured",
			flags:   RunFlags{API: true},
			wantErr: true,
		},
		{
			// Whitespace is not a credential, and an operator who sets the key to an
			// empty string has configured nothing while believing otherwise.
			name:    "the API is served with a blank token",
			flags:   RunFlags{API: true},
			token:   "   ",
			wantErr: true,
		},
		{
			name:  "the API is served with a token",
			flags: RunFlags{API: true},
			token: "s3cr3t",
		},
		{
			name:  "this process serves no API",
			flags: RunFlags{Dispatcher: true, Sender: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			if tc.token != "" {
				viper.Set(container.APIAdminTokenKey, tc.token)
			}

			err := requireAdminToken(tc.flags)

			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected the boot to be allowed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the boot to be refused")
			}
			for _, want := range []string{container.APIAdminTokenKey, container.APIAdminTokenEnvVar} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the refusal to name %q, got %q", want, err)
				}
			}
		})
	}
}
