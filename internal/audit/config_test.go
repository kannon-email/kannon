package audit

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestTryLoadConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("KANNON_TEST_AUDIT_RETENTION", "48h")
	viper.Set("audit.enabled", true)
	viper.Set("audit.retention", "env://KANNON_TEST_AUDIT_RETENTION")

	cfg, err := TryLoadConfig()
	if err != nil {
		t.Fatalf("TryLoadConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Enabled = false, want the configured value")
	}
	if cfg.Retention.Hours() != 48 {
		t.Errorf("Retention = %v, want the referenced 48h", cfg.Retention)
	}
}

func TestTryLoadConfig_DefaultRetention(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := TryLoadConfig()
	if err != nil {
		t.Fatalf("TryLoadConfig: %v", err)
	}
	if cfg.Retention != DefaultRetention {
		t.Errorf("Retention = %v, want %v", cfg.Retention, DefaultRetention)
	}
}

// The reason TryLoadConfig exists: the API reads this section from inside its own
// runnable's goroutine, so a section it cannot resolve has to come back as an error
// it can log and step over. A panic there would be recovered by nobody, and an
// audit trail Kannon cannot write would become an outage for somebody's customers.
func TestTryLoadConfig_ReturnsAnErrorRatherThanPanicking(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TryLoadConfig panicked instead of returning: %v", r)
		}
	}()

	viper.Set("audit.retention", "env://KANNON_TEST_AUDIT_RETENTION_NOBODY_SET")

	_, err := TryLoadConfig()
	if err == nil {
		t.Fatal("expected an error naming the variable")
	}
	if !strings.Contains(err.Error(), "KANNON_TEST_AUDIT_RETENTION_NOBODY_SET") {
		t.Errorf("expected the error to name the variable, got %v", err)
	}
}

// And LoadConfig keeps the boot path's contract, where the operator's file being
// wrong should stop the process rather than yield a zero value.
func TestLoadConfig_PanicsOnAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("audit.retention", "env://KANNON_TEST_AUDIT_RETENTION_NOBODY_SET")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic naming the variable")
		}
		err, ok := r.(error)
		if !ok || !strings.Contains(err.Error(), "KANNON_TEST_AUDIT_RETENTION_NOBODY_SET") {
			t.Errorf("expected the panic to name the variable, got %v", r)
		}
	}()
	LoadConfig()
}
