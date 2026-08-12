// Package envref resolves `env://NAME` / `env://NAME:-default` references found
// in configuration values, as a mapstructure decode hook plugged into every
// unmarshal x/config performs.
//
//	api:
//	  port: "env://KANNON_API_PORT:-50051"    # default when the var is not set
//	  admin_token: "env://KANNON_ADMIN_TOKEN" # required, fails fast when not set
//
// It exists because viper's own environment support cannot reach a nested key:
// AutomaticEnv answers Get("api.port") from the environment, but UnmarshalKey —
// which is how every runnable reads its section — never consults it at all, so a
// nested key was silently ignored. Rather than bind every key by hand, the config
// file names the variable it wants: which settings come from the environment is
// then the operator's decision, visible in one file, and the variable can be
// called whatever the deployment already calls it.
//
// The reference must span the whole leaf value: `env://NAME` is resolved,
// `https://env://NAME/v1` is not. A value that opens with the scheme and is not
// a well-formed reference — `env:/NAME`, `ENV://NAME`, `env://my-var`,
// `env://NAME:default` — is refused rather than passed through as a literal,
// because it is a typo in a reference and never a value Kannon has a use for. A
// literal that really does start with `env://` can be escaped as `\env://...`.
package envref

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// envRefPattern captures the env var name and, optionally, an inline default.
// The name character class stops at the ":-" separator, so a default is free to
// contain colons and slashes (`env://DSN:-postgres://localhost:5432/app`). The
// default group may be empty (`env://NAME:-` means "empty string") and may span
// newlines, which is why it is matched with (?s:.*).
var envRefPattern = regexp.MustCompile(`^env://([A-Za-z_][A-Za-z0-9_]*)(?::-((?s:.*)))?$`)

// envSchemePattern matches a value that was meant to be a reference, whether or
// not it is a well-formed one. Anything it matches and envRefPattern does not is
// a mistake in the spelling, and treating it as a literal is how the text of a
// ConfigMap came to be usable as an admin token: `admin_token:
// env://kannon-admin-token` resolved to itself, and a non-blank string is all
// the boot check asks for. Deliberately narrower than the scheme alone — a value
// opening with `env:` and no slash is left as a literal.
var envSchemePattern = regexp.MustCompile(`(?i)^env:/`)

// MissingEnvError is returned when a reference without an inline default points
// at an env var that is not set. mapstructure wraps it with the config key, so
// errors.As still finds it after the unmarshal.
type MissingEnvError struct {
	// Name is the env var that was looked up.
	Name string
	// Ref is the raw config value that referenced it.
	Ref string
}

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required env var %q is not set (referenced by %q)", e.Name, e.Ref)
}

// MalformedRefError is returned for a value that opens with the reference scheme
// and is not a reference. It is an error rather than a literal because the whole
// promise of the syntax is that a variable nobody set stops the boot, and a
// reference nobody can parse would fail that promise open — silently, on the one
// key where silence costs the most.
type MalformedRefError struct {
	// Ref is the raw config value.
	Ref string
}

func (e *MalformedRefError) Error() string {
	return fmt.Sprintf("malformed reference %q: write env://NAME or env://NAME:-default, where NAME "+
		"matches [A-Za-z_][A-Za-z0-9_]* and an inline default is introduced by `:-` "+
		`(a literal value starting with the scheme is escaped as \env://...)`, e.Ref)
}

// Options tunes resolution.
type Options struct {
	// EmptyIsUnset treats a set-but-empty env var as unset, mirroring POSIX
	// `${NAME:-default}`. When false an empty env var is a legitimate value
	// and wins over the inline default.
	EmptyIsUnset bool

	// Lookup replaces os.LookupEnv. Handy for tests and for reading from a
	// vault-like source instead of the process environment.
	Lookup func(string) (string, bool)
}

// kannonOptions is the policy behind Decoder and Resolve, so that a reference
// means the same thing in every section of an operator's file.
//
// EmptyIsUnset is on: it matches POSIX `${NAME:-default}`, which is the syntax
// this borrows and therefore what an operator will expect, and in a container a
// variable left empty is a hole in the deployment — an `env:` entry wired to a
// value nobody filled in — far more often than it is a considered empty setting.
// The one key where an empty string could pass for a configured value is
// api.admin_token, and a blank token is refused at boot either way.
var kannonOptions = Options{EmptyIsUnset: true}

// Decoder is the decoder option every unmarshal in x/config passes to viper.
func Decoder() viper.DecoderConfigOption {
	return DecoderOption(kannonOptions)
}

// Hook returns a decode hook that rewrites every string leaf holding a
// reference. It deliberately does not look at the destination type, so
// `port: env://KANNON_API_PORT` still lands in an int field: the resolved string
// is handed back to the decoder, which converts it (viper enables
// WeaklyTypedInput) or fails with its usual type error.
func Hook(opts Options) mapstructure.DecodeHookFunc {
	return func(f reflect.Type, _ reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		// Not data.(string): the source may be a named string type, e.g. a
		// value pushed in with viper.Set.
		value, isRef, err := resolve(reflect.ValueOf(data).String(), opts)
		if err != nil {
			return nil, err
		}
		if !isRef {
			// data rather than value, so a named string type survives a leaf
			// that was never a reference in the first place.
			return data, nil
		}
		return value, nil
	}
}

// resolve reports whether raw is a whole-value reference and, if so, what it
// stands for.
func resolve(raw string, opts Options) (value string, isRef bool, err error) {
	lookup := opts.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if rest, ok := strings.CutPrefix(raw, `\env://`); ok {
		return `env://` + rest, true, nil
	}

	// FindStringSubmatchIndex, not FindStringSubmatch: only the indices tell an
	// absent default group (-1) from an empty one.
	idx := envRefPattern.FindStringSubmatchIndex(raw)
	if idx == nil {
		if envSchemePattern.MatchString(raw) {
			return "", true, &MalformedRefError{Ref: raw}
		}
		return raw, false, nil
	}
	name := raw[idx[2]:idx[3]]
	hasDefault := idx[4] >= 0

	v, found := lookup(name)
	if found && opts.EmptyIsUnset && v == "" {
		found = false
	}
	switch {
	case found:
		return v, true, nil
	case hasDefault:
		return raw[idx[4]:idx[5]], true, nil
	default:
		return "", true, &MissingEnvError{Name: name, Ref: raw}
	}
}

// DecoderOption wires the hook into viper.Unmarshal / viper.UnmarshalKey.
//
// It re-adds the hooks viper installs by default, because viper.DecodeHook
// replaces the whole DecodeHook chain rather than appending to it: passing the
// env hook alone would silently break time.Duration fields — stats.retention,
// audit.retention, every smtp timeout — and comma-separated slices.
func DecoderOption(opts Options) viper.DecoderConfigOption {
	return viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		Hook(opts),
		mapstructure.StringToTimeDurationHookFunc(),
		stringToWeakSliceHookFunc(","),
	))
}

// stringToWeakSliceHookFunc is viper's own string-to-slice hook, reproduced here
// because it is unexported there.
//
// mapstructure.StringToSliceHookFunc is deliberately not used, and viper does not
// use it either: since mapstructure v2 it splits only when the destination is
// exactly []string, so a comma-separated value decoded into []time.Duration or a
// named slice type would stop being split here while the same YAML kept working
// everywhere viper's default chain is used. Nothing in Kannon's config is such a
// field today, which is precisely why the divergence would be found late.
func stringToWeakSliceHookFunc(sep string) mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String || t.Kind() != reflect.Slice {
			return data, nil
		}
		// Not data.(string), for the reason Hook says: a named string type
		// reaches the chain when a value arrives through viper.Set.
		raw := reflect.ValueOf(data).String()
		if raw == "" {
			return []string{}, nil
		}
		return strings.Split(raw, sep), nil
	}
}
