package envref

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// The destination types mirror the shape of Kannon's own sections — a duration,
// a port, a bool, a nested struct — so the cases exercise what the real config
// actually asks of the hook.
type senderConfig struct {
	Hostname   string `mapstructure:"hostname"`
	DemoSender bool   `mapstructure:"demo_sender"`
}

type testConfig struct {
	DatabaseURL string            `mapstructure:"database_url"`
	Port        int               `mapstructure:"port"`
	Debug       bool              `mapstructure:"debug"`
	Retention   time.Duration     `mapstructure:"retention"`
	Hosts       []string          `mapstructure:"hosts"`
	Labels      map[string]string `mapstructure:"labels"`
	Replicas    *int              `mapstructure:"replicas"`
	Sender      senderConfig      `mapstructure:"sender"`
}

// lookupFrom builds an Options.Lookup backed by a map, so the table tests do
// not have to mutate the process environment.
func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func newViper(t *testing.T, yaml string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	return v
}

func load(t *testing.T, yaml string, opts Options, out any) error {
	t.Helper()
	return newViper(t, yaml).Unmarshal(out, DecoderOption(opts))
}

func mustLoad(t *testing.T, yaml string, opts Options, out any) {
	t.Helper()
	if err := load(t, yaml, opts, out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
}

func TestStringLeafResolution(t *testing.T) {
	env := map[string]string{
		"KANNON_DATABASE_URL": "postgres://kannon@db/kannon",
		"EMPTY":               "",
	}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"env var set", "env://KANNON_DATABASE_URL", "postgres://kannon@db/kannon"},
		{"env var set, default ignored", "env://KANNON_DATABASE_URL:-fallback", "postgres://kannon@db/kannon"},
		{"env var missing, inline default", "env://NOPE:-fallback", "fallback"},
		{"empty inline default means empty string", "env://NOPE:-", ""},
		{"set-but-empty wins over default by default", "env://EMPTY:-fallback", ""},
		{"default may be a url", "env://NOPE:-postgres://u:p@h:5432/db?ssl=1", "postgres://u:p@h:5432/db?ssl=1"},
		{"default may contain :-", "env://NOPE:-a:-b", "a:-b"},
		{"plain value is untouched", "postgres://localhost/kannon", "postgres://localhost/kannon"},
		{"reference must span the whole value", "https://env://HOST/v1", "https://env://HOST/v1"},
		{"a value opening with env: and no slash is a literal", "env:KANNON_DATABASE_URL", "env:KANNON_DATABASE_URL"},
		{"backslash escapes a literal", `\env://KANNON_DATABASE_URL`, "env://KANNON_DATABASE_URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg testConfig
			mustLoad(t, "database_url: '"+tc.value+"'\n", Options{Lookup: lookupFrom(env)}, &cfg)
			if cfg.DatabaseURL != tc.want {
				t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, tc.want)
			}
		})
	}
}

// A reference an operator misspelled is refused rather than handed to the code as
// a literal. Every case here used to resolve to itself, which on api.admin_token
// meant a process authenticating callers against the text of its own ConfigMap.
func TestMalformedReferenceIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"a name an env var cannot have", "env://kannon-admin-token"},
		{"a single slash", "env:/KANNON_DATABASE_URL"},
		{"the scheme in upper case", "ENV://KANNON_DATABASE_URL"},
		{"a default introduced with : instead of :-", "env://KANNON_DATABASE_URL:fallback"},
		{"no name at all", "env://"},
		{"a path after the name", "env://KANNON_DATABASE_URL/v1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A lookup that would answer, so the refusal is about the spelling
			// and not about a variable being unset.
			resolvable := map[string]string{"KANNON_DATABASE_URL": "postgres://kannon@db/kannon"}
			err := load(t, "database_url: '"+tc.value+"'\n", Options{Lookup: lookupFrom(resolvable)}, &testConfig{})

			var malformed *MalformedRefError
			if !errors.As(err, &malformed) {
				t.Fatalf("expected *MalformedRefError for %q, got %v", tc.value, err)
			}
			if malformed.Ref != tc.value {
				t.Errorf("Ref = %q, want %q", malformed.Ref, tc.value)
			}
			// The message has to be enough to fix the file from, since it is all
			// the operator gets.
			if !strings.Contains(err.Error(), "env://NAME:-default") {
				t.Errorf("the message does not show the spelling: %v", err)
			}
		})
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{raw: "env://KANNON_DATABASE_URL", want: "KANNON_DATABASE_URL", wantOK: true},
		{raw: "env://K_DATABASE_URL:-postgres://localhost/kannon", want: "K_DATABASE_URL", wantOK: true},
		{raw: "postgres://localhost/kannon"},
		{raw: `\env://KANNON_DATABASE_URL`},
		{raw: "env://not-a-name"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := Name(tc.raw)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("Name(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestUnquotedReferenceIsValidYAML makes sure the scheme-like syntax survives
// YAML parsing without quotes, since nothing forces an operator to quote it and
// the examples Kannon ships do not.
func TestUnquotedReferenceIsValidYAML(t *testing.T) {
	var cfg testConfig
	mustLoad(t, "database_url: env://KANNON_DATABASE_URL:-postgres://localhost/kannon\n", Options{Lookup: lookupFrom(nil)}, &cfg)
	if cfg.DatabaseURL != "postgres://localhost/kannon" {
		t.Errorf("DatabaseURL = %q, want the inline default", cfg.DatabaseURL)
	}
}

func TestLookupDefaultsToProcessEnv(t *testing.T) {
	t.Setenv("KANNON_DATABASE_URL", "from-os-env")

	var cfg testConfig
	mustLoad(t, "database_url: env://KANNON_DATABASE_URL\n", Options{}, &cfg)

	if cfg.DatabaseURL != "from-os-env" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "from-os-env")
	}
}

// A reference with no default naming a variable nobody set stops the boot, and
// the message names both halves of what the operator got wrong: the config key
// and the variable it asked for.
func TestMissingEnvVarFailsFast(t *testing.T) {
	var cfg testConfig
	err := load(t, "sender:\n  hostname: env://KANNON_SENDER_HOSTNAME\n", Options{Lookup: lookupFrom(nil)}, &cfg)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var missing *MissingEnvError
	if !errors.As(err, &missing) {
		t.Fatalf("error is not a *MissingEnvError: %T", err)
	}
	if missing.Name != "KANNON_SENDER_HOSTNAME" {
		t.Errorf("Name = %q, want %q", missing.Name, "KANNON_SENDER_HOSTNAME")
	}
	// mapstructure prefixes the failing leaf with its config key, which is what
	// makes the message actionable: it names both the key and the variable.
	if !strings.Contains(err.Error(), "sender.hostname") {
		t.Errorf("error does not mention the config key: %v", err)
	}
}

func TestNonStringDestinations(t *testing.T) {
	env := map[string]string{
		"PORT":      "8443",
		"DEBUG":     "true",
		"RETENTION": "1500ms",
		"REPLICAS":  "3",
	}
	const yaml = `
port: env://PORT
debug: env://DEBUG
retention: env://RETENTION
replicas: env://REPLICAS
`
	var cfg testConfig
	mustLoad(t, yaml, Options{Lookup: lookupFrom(env)}, &cfg)

	if cfg.Port != 8443 {
		t.Errorf("Port = %d, want 8443", cfg.Port)
	}
	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
	if cfg.Retention != 1500*time.Millisecond {
		t.Errorf("Retention = %v, want 1.5s", cfg.Retention)
	}
	if cfg.Replicas == nil || *cfg.Replicas != 3 {
		t.Errorf("Replicas = %v, want 3", cfg.Replicas)
	}
}

// TestBadValueFromEnvFailsWithTypeError covers the other half of type
// conversion: a resolved value that cannot become an int must still be caught.
func TestBadValueFromEnvFailsWithTypeError(t *testing.T) {
	err := load(t, "port: env://PORT\n", Options{Lookup: lookupFrom(map[string]string{"PORT": "not-a-number"})}, &testConfig{})
	if err == nil {
		t.Fatal("expected a type error, got nil")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("error does not mention the field: %v", err)
	}
}

// Regression for the trap DecoderOption exists to avoid: viper.DecodeHook
// replaces the whole chain, so a bare env hook would break every duration in
// Kannon's config — stats.retention, audit.retention, the smtp timeouts.
func TestBuiltinViperHooksStillWork(t *testing.T) {
	const yaml = `
retention: 720h
hosts: a.example.com,env://HOST_B
`
	var cfg testConfig
	mustLoad(t, yaml, Options{Lookup: lookupFrom(map[string]string{"HOST_B": "b.example.com"})}, &cfg)

	if cfg.Retention != 720*time.Hour {
		t.Errorf("Retention = %v, want 720h", cfg.Retention)
	}
	if want := []string{"a.example.com", "b.example.com"}; !reflect.DeepEqual(cfg.Hosts, want) {
		t.Errorf("Hosts = %#v, want %#v", cfg.Hosts, want)
	}
}

func TestSliceAndMapElements(t *testing.T) {
	const yaml = `
hosts:
  - env://HOST_A
  - env://HOST_B:-b.example.com
labels:
  team: env://TEAM
  tier: env://TIER:-free
`
	var cfg testConfig
	mustLoad(t, yaml, Options{Lookup: lookupFrom(map[string]string{
		"HOST_A": "a.example.com",
		"TEAM":   "platform",
	})}, &cfg)

	if want := []string{"a.example.com", "b.example.com"}; !reflect.DeepEqual(cfg.Hosts, want) {
		t.Errorf("Hosts = %#v, want %#v", cfg.Hosts, want)
	}
	if want := map[string]string{"team": "platform", "tier": "free"}; !reflect.DeepEqual(cfg.Labels, want) {
		t.Errorf("Labels = %#v, want %#v", cfg.Labels, want)
	}
}

// Decoder is the option the rest of Kannon uses, and it turns EmptyIsUnset on:
// a variable wired into a container but left empty is a hole in the deployment,
// so the inline default applies and a reference without one refuses the boot.
func TestDecoderTreatsEmptyAsUnset(t *testing.T) {
	t.Setenv("KANNON_SENDER_HOSTNAME", "")

	var cfg testConfig
	if err := newViper(t, "sender:\n  hostname: env://KANNON_SENDER_HOSTNAME:-localhost\n").Unmarshal(&cfg, Decoder()); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.Sender.Hostname != "localhost" {
		t.Errorf("Hostname = %q, want the inline default", cfg.Sender.Hostname)
	}

	err := newViper(t, "sender:\n  hostname: env://KANNON_SENDER_HOSTNAME\n").Unmarshal(&testConfig{}, Decoder())
	var missing *MissingEnvError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingEnvError for an empty var without default, got %v", err)
	}
}

func TestEmptyIsUnsetIsOptional(t *testing.T) {
	env := map[string]string{"TOKEN": ""}

	var cfg testConfig
	mustLoad(t, "database_url: env://TOKEN:-fallback\n", Options{Lookup: lookupFrom(env)}, &cfg)
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q; with EmptyIsUnset off an empty var is a value", cfg.DatabaseURL)
	}
}

// The legacy K_ variables are promoted onto their keys with viper.Set, and
// standalone forces use_embedded_nats the same way, so the override layer has to
// go through the hook as well.
func TestDefaultsAndOverridesAreResolvedToo(t *testing.T) {
	v := newViper(t, "port: 8080\n")
	v.SetDefault("sender.hostname", "env://KANNON_SENDER_HOSTNAME:-localhost")
	v.Set("database_url", "env://DSN")

	var cfg testConfig
	if err := v.Unmarshal(&cfg, DecoderOption(Options{
		Lookup: lookupFrom(map[string]string{"DSN": "postgres://localhost/kannon"}),
	})); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if cfg.Sender.Hostname != "localhost" {
		t.Errorf("Hostname = %q, want %q (viper defaults go through the hook)", cfg.Sender.Hostname, "localhost")
	}
	if cfg.DatabaseURL != "postgres://localhost/kannon" {
		t.Errorf("DatabaseURL = %q, want the resolved DSN (viper overrides go through the hook)", cfg.DatabaseURL)
	}
}

// UnmarshalKey is how every runnable reads its section, so it has to resolve
// references too.
func TestUnmarshalKey(t *testing.T) {
	v := newViper(t, "sender:\n  hostname: env://HOST:-localhost\n  demo_sender: env://DEMO:-true\n")

	var sender senderConfig
	if err := v.UnmarshalKey("sender", &sender, DecoderOption(Options{Lookup: lookupFrom(nil)})); err != nil {
		t.Fatalf("UnmarshalKey: %v", err)
	}
	if sender.Hostname != "localhost" || !sender.DemoSender {
		t.Errorf("sender = %#v, want the inline defaults", sender)
	}
}

// TestGetBypassesTheHook documents the boundary of the approach, and the reason
// Kannon reads its configuration through structs rather than through viper's
// accessors: the hook only runs while decoding, so Get returns the raw
// reference. A viper.GetString added back for a key an operator may write a
// reference into would hand that reference to the code as a value.
func TestGetBypassesTheHook(t *testing.T) {
	v := newViper(t, "database_url: env://KANNON_DATABASE_URL:-postgres://localhost/kannon\n")
	if got := v.GetString("database_url"); got != "env://KANNON_DATABASE_URL:-postgres://localhost/kannon" {
		t.Errorf("GetString = %q; the hook is not expected to run here", got)
	}
}

// TestMapKeysAlsoGoThroughTheHook documents a sharp edge: mapstructure decodes
// map keys through the hook as well, and viper has already lower-cased them.
// Nothing in Kannon's config is a map with operator-chosen keys today.
func TestMapKeysAlsoGoThroughTheHook(t *testing.T) {
	var cfg testConfig
	err := load(t, "labels:\n  env://TEAM: platform\n", Options{Lookup: lookupFrom(nil)}, &cfg)
	if err == nil {
		t.Fatalf("expected the key to be treated as a reference, got %#v", cfg.Labels)
	}
}

// A named string type reaches the hook when a value arrives through viper.Set
// rather than out of the file, which is how a test — or any caller writing into
// viper's override layer — supplies one.
func TestHookHandlesNamedStringInput(t *testing.T) {
	type level string

	v := newViper(t, "port: 1\n")
	v.Set("database_url", level("env://DSN:-postgres://localhost/kannon"))

	var cfg testConfig
	if err := v.Unmarshal(&cfg, DecoderOption(Options{Lookup: lookupFrom(nil)})); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/kannon" {
		t.Errorf("DatabaseURL = %q, want the inline default", cfg.DatabaseURL)
	}
}
