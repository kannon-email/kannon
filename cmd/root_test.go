package cmd

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/x/config"
	"github.com/spf13/viper"
)

// A process asked to serve the API without a credential for it is refused at boot, and one serving
// only workers is not — the credential belongs on the hosts that answer admin requests and nowhere
// else. The refusal names the key and the environment spelling it can be referenced from, since that
// message is all the operator gets.
func TestRequireAdminToken(t *testing.T) {
	tests := []struct {
		name     string
		services config.Services
		token    string
		wantErr  bool
	}{
		{
			name:     "the API is served with no token configured",
			services: config.Services{API: config.Service{Enabled: true}},
			wantErr:  true,
		},
		{
			// Whitespace is not a credential, and an operator who sets the key to an
			// empty string has configured nothing while believing otherwise.
			name:     "the API is served with a blank token",
			services: config.Services{API: config.Service{Enabled: true}},
			token:    "   ",
			wantErr:  true,
		},
		{
			name:     "the API is served with a token",
			services: config.Services{API: config.Service{Enabled: true}},
			token:    "s3cr3t",
		},
		{
			name: "this process serves no API",
			services: config.Services{
				Dispatcher: config.Service{Enabled: true},
				Sender:     config.Service{Enabled: true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			if tc.token != "" {
				viper.Set(config.APIAdminTokenKey, tc.token)
			}

			err := requireAdminToken(tc.services)

			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected the boot to be allowed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the boot to be refused")
			}
			for _, want := range []string{config.APIAdminTokenKey, config.APIAdminTokenEnvVar} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the refusal to name %q, got %q", want, err)
				}
			}
		})
	}
}

// Every --run-* flag is deprecated and every one of them still works, so every one of them has to
// stay in --help: pflag hides a flag when it is marked deprecated, and an operator migrating one
// Deployment at a time needs to be able to read the spellings out of the binary.
func TestDeprecatedRunFlagsStayVisible(t *testing.T) {
	flags := rootCmd.PersistentFlags()

	for _, name := range []string{
		"run-sender", "run-dispatcher", "run-validator", "run-verifier",
		"run-tracker", "run-bounce", "run-stats", "run-api", "run-smtp", "run-audit",
	} {
		flag := flags.Lookup(name)
		if flag == nil {
			t.Errorf("--%s is gone; it is deprecated, not removed", name)
			continue
		}
		if flag.Deprecated == "" {
			t.Errorf("--%s is not marked deprecated", name)
		}
		if flag.Hidden {
			t.Errorf("--%s is hidden, so --help no longer names a flag that still works", name)
		}
	}
}

// A token an operator asked to come from a variable nobody set is refused the same way a missing
// one is: what the API must never do is come up answering every admin request with unauthenticated.
func TestRequireAdminTokenWithAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(config.APIAdminTokenKey, "env://KANNON_ADMIN_TOKEN_NOBODY_SET")

	err := requireAdminToken(config.Services{API: config.Service{Enabled: true}})
	if err == nil {
		t.Fatal("expected the boot to be refused")
	}
	if !strings.Contains(err.Error(), "KANNON_ADMIN_TOKEN_NOBODY_SET") {
		t.Errorf("expected the refusal to name the variable, got %q", err)
	}
}
