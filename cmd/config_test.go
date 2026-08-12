package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kannon-email/kannon/x/config"
	"github.com/spf13/viper"
)

// The order of the two reads is the cmd layer's to get wrong: `services` is a section of
// the file, so reading it before config.Read has loaded that file unmarshals an empty
// viper, nothing is registered, and the process exits immediately after logging
// "Starting Kannon runnables: []".
func TestReadConfigAndServices_ReadsTheFileBeforeTheServicesSection(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	// The developer's own ~/.kannon.yaml is not part of this: readConfig reads
	// whatever viper has been pointed at, and here that is the file below.
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "kannon.yaml")
	if err := os.WriteFile(path, []byte("services:\n  api:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	config.Prepare(path)

	_, services, err := readConfigAndServices()
	if err != nil {
		t.Fatalf("readConfigAndServices: %v", err)
	}

	if got := services.Enabled(); len(got) != 1 || got[0] != "api" {
		t.Errorf("enabled services = %v, want [api] from the config file", got)
	}
}
