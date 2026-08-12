package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// writeConfig points viper at a config file holding yaml, the way --config does.
func writeConfig(t *testing.T, yaml string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kannon.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	Prepare(path)
}

// prepareWithoutAConfigFile is Prepare("") — a process started with no --config —
// with the home directory pointed at an empty one. Without that, viper searches
// the developer's own $HOME and a ~/.kannon.yaml sitting there decides the result:
// the same leakage from the machine running the tests that the environ hook exists
// to keep out of the deprecation warning.
func prepareWithoutAConfigFile(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	Prepare("")
}

func TestLoadSection_HappyPath(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("foo.name", "alice")
	viper.Set("foo.count", 7)

	var out struct {
		Name  string `mapstructure:"name"`
		Count int    `mapstructure:"count"`
	}
	LoadSection("foo", &out)

	if out.Name != "alice" || out.Count != 7 {
		t.Errorf("unexpected unmarshal result: %+v", out)
	}
}

func TestLoadSection_PanicsOnTypeMismatch(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("foo.count", "not-a-number")

	var out struct {
		Count int `mapstructure:"count"`
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on type mismatch")
		}
		msg, ok := r.(error)
		if !ok || !strings.Contains(msg.Error(), "foo") {
			t.Errorf("expected panic mentioning key, got %v", r)
		}
	}()
	LoadSection("foo", &out)
}

// The point of the whole exercise: a nested key can be taken from the
// environment, which is what viper could not do through UnmarshalKey.
func TestLoadSection_ResolvesEnvReferences(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("KANNON_TEST_HOSTNAME", "mail.example.com")

	viper.Set("sender.hostname", "env://KANNON_TEST_HOSTNAME")
	viper.Set("sender.max_jobs", "env://KANNON_TEST_MAX_JOBS:-100")

	var out struct {
		Hostname string `mapstructure:"hostname"`
		MaxJobs  uint   `mapstructure:"max_jobs"`
	}
	LoadSection("sender", &out)

	if out.Hostname != "mail.example.com" {
		t.Errorf("Hostname = %q, want the value of KANNON_TEST_HOSTNAME", out.Hostname)
	}
	if out.MaxJobs != 100 {
		t.Errorf("MaxJobs = %d, want the inline default", out.MaxJobs)
	}
}

func TestLoadSection_PanicsOnAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("sender.hostname", "env://KANNON_TEST_HOSTNAME_NOBODY_SET")

	var out struct {
		Hostname string `mapstructure:"hostname"`
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic naming the variable")
		}
		err, ok := r.(error)
		if !ok || !strings.Contains(err.Error(), "KANNON_TEST_HOSTNAME_NOBODY_SET") {
			t.Errorf("expected the panic to name the variable, got %v", r)
		}
	}()
	LoadSection("sender", &out)
}

func TestRead_FromTheConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("KANNON_TEST_DSN", "postgres://kannon@db/kannon")

	writeConfig(t, `
database_url: env://KANNON_TEST_DSN
nats_url: env://KANNON_TEST_NATS:-nats://localhost:4222
debug: env://KANNON_TEST_DEBUG:-true
`)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if cfg.DatabaseURL != "postgres://kannon@db/kannon" {
		t.Errorf("DatabaseURL = %q, want the value of KANNON_TEST_DSN", cfg.DatabaseURL)
	}
	if cfg.NatsURL != "nats://localhost:4222" {
		t.Errorf("NatsURL = %q, want the inline default", cfg.NatsURL)
	}
	if !cfg.Debug {
		t.Error("Debug = false, want the inline default of true")
	}
}

// A missing file is not an error: every setting in it can come from the
// environment, and `kannon standalone` with a database URL and nothing else is a
// supported way to run.
func TestRead_ToleratesAMissingFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	prepareWithoutAConfigFile(t)

	if _, err := Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestRead_ReportsAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	writeConfig(t, "database_url: env://KANNON_TEST_DSN_NOBODY_SET\n")

	_, err := Read()
	if err == nil {
		t.Fatal("expected an error naming the variable")
	}
	if !strings.Contains(err.Error(), "KANNON_TEST_DSN_NOBODY_SET") {
		t.Errorf("expected the error to name the variable, got %v", err)
	}
}

// The credential the file names is the credential the API gets, next to the keys
// beside it: `api` is the one section where a value and a reference have always
// had to coexist, and the section read is what resolves both.
func TestRead_ResolvesTheAdminTokenReferenceBesideTheRestOfTheSection(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("KANNON_ADMIN_TOKEN", "rotated-token")

	writeConfig(t, `
database_url: postgres://from-the-file/kannon
api:
  port: 50052
  admin_token: env://KANNON_ADMIN_TOKEN
`)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cfg.DatabaseURL != "postgres://from-the-file/kannon" {
		t.Errorf("DatabaseURL = %q, want the value in the file", cfg.DatabaseURL)
	}

	token, err := APIAdminToken()
	if err != nil {
		t.Fatalf("APIAdminToken: %v", err)
	}
	if token != "rotated-token" {
		t.Errorf("APIAdminToken() = %q, want the variable the file names", token)
	}

	var api struct {
		Port uint `mapstructure:"port"`
	}
	LoadSection("api", &api)
	if api.Port != 50052 {
		t.Errorf("api.port = %d, want the value in the file", api.Port)
	}
}

// There is no default, and an unset key must answer with nothing rather than with something a
// caller could authenticate against. Asserted with no key set at all, because a reader running
// without the cmd layer's config setup must still get the documented answer.
func TestAPIAdminToken_EmptyWhenUnset(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	got, err := APIAdminToken()
	if err != nil {
		t.Fatalf("APIAdminToken: %v", err)
	}
	if got != "" {
		t.Errorf("expected no admin token when the key is unset, got %q", got)
	}
}

func TestAPIAdminToken_FromTheConfigFileKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(APIAdminTokenKey, "s3cr3t")

	got, err := APIAdminToken()
	if err != nil {
		t.Fatalf("APIAdminToken: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("APIAdminToken() = %q, want %q", got, "s3cr3t")
	}
}

// A reference is what the key is meant to hold now: the secret stays in a Secret
// and the file names it, under whatever the deployment already calls it.
func TestAPIAdminToken_FromAReferenceInTheConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("KANNON_TEST_ADMIN_TOKEN", "s3cr3t-from-a-reference")
	viper.Set(APIAdminTokenKey, "env://KANNON_TEST_ADMIN_TOKEN")

	got, err := APIAdminToken()
	if err != nil {
		t.Fatalf("APIAdminToken: %v", err)
	}
	if got != "s3cr3t-from-a-reference" {
		t.Errorf("APIAdminToken() = %q, want the referenced value", got)
	}
}

func TestAPIAdminToken_ReportsAnUnresolvableReference(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(APIAdminTokenKey, "env://KANNON_TEST_ADMIN_TOKEN_NOBODY_SET")

	if _, err := APIAdminToken(); err == nil {
		t.Fatal("expected an error naming the variable")
	}
}
