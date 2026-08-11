package cmd

import (
	"testing"

	"github.com/spf13/viper"
)

// Regression: --run-bounce / --run-verifier are promoted onto their canonical names
// by config.Read, and config.LoadServices is what reads those names. Asserted
// through the cmd layer's own entry point, because the order of those two reads is
// the cmd layer's to get wrong — and when it is wrong the alias fires too late,
// nothing is registered, and the process exits immediately after logging
// "Starting Kannon runnables: []".
func TestReadConfigAndServices_PromotesDeprecatedAliasesFirst(t *testing.T) {
	tests := []struct {
		alias string
		want  string
	}{
		{alias: "run-bounce", want: "tracker"},
		{alias: "run-verifier", want: "validator"},
	}

	for _, tc := range tests {
		t.Run(tc.alias, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			// The developer's own ~/.kannon.yaml is not part of this: readConfig
			// reads whatever viper has been pointed at, and here that is nothing.
			t.Setenv("HOME", t.TempDir())

			viper.Set(tc.alias, true)

			_, services, err := readConfigAndServices()
			if err != nil {
				t.Fatalf("readConfigAndServices: %v", err)
			}

			if got := services.Enabled(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("enabled services = %v, want [%s] after promoting %q", got, tc.want, tc.alias)
			}
		})
	}
}
