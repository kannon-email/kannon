package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestByteSize_ParsesKubernetesQuantities(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ByteSize
	}{
		{"512000", 512000},
		{"0", 0},
		{"500Ki", 512000},
		{"5Mi", 5242880},
		{"1Gi", 1073741824},
		{"2k", 2000},
		{"5M", 5000000},
		{"1G", 1000000000},
	} {
		var got ByteSize
		if err := got.UnmarshalText([]byte(tc.in)); err != nil {
			t.Errorf("%q: unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestByteSize_RefusesWhatKubernetesWouldNotRead(t *testing.T) {
	for _, in := range []string{"", "5MB", "5 Mi", "5mi", "5K", "-1Mi", "1.5Mi", "Mi", "9999999999Gi"} {
		var got ByteSize
		if err := got.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("%q: want an error, got %d", in, got)
		}
	}
}

// A size reaches a section from the file as a quantity, as a bare integer, or through a reference.
func TestLoadSection_DecodesByteSizes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("KANNON_TEST_SIZE", "2Mi")
	writeConfig(t, `
api:
  quantity: 5Mi
  integer: 512000
  reference: env://KANNON_TEST_SIZE
  fallback: env://KANNON_TEST_UNSET_SIZE:-500Ki
`)
	if _, err := Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}

	var out struct {
		Quantity  ByteSize `mapstructure:"quantity"`
		Integer   ByteSize `mapstructure:"integer"`
		Reference ByteSize `mapstructure:"reference"`
		Fallback  ByteSize `mapstructure:"fallback"`
	}
	LoadSection("api", &out)

	if out.Quantity != 5242880 || out.Integer != 512000 || out.Reference != 2097152 || out.Fallback != 512000 {
		t.Errorf("unexpected sizes: %+v", out)
	}
}

func TestLoadSection_RefusesAMalformedByteSize(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("api.max_payload", "5MB")

	var out struct {
		MaxPayload ByteSize `mapstructure:"max_payload"`
	}
	if err := TryLoadSection("api", &out); err == nil || !strings.Contains(err.Error(), "5MB") {
		t.Fatalf("want an error naming the value, got %v", err)
	}
}
