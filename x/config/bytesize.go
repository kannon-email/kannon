package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ByteSize is a size in bytes, written the way Kubernetes writes a quantity so that the same file
// reads the same way as the manifests beside it: `5Mi`, `500Ki`, `1G`. Binary suffixes (Ki, Mi, Gi)
// are powers of 1024, decimal ones (k, M, G) powers of 1000, and a bare number is bytes, so an
// integer written before this type existed still means what it did.
//
// Only whole, non-negative numbers are read; anything else — `5MB`, `5 Mi`, `1.5Mi` — is refused
// at boot rather than read as something the operator did not write.
type ByteSize int64

var byteSizeSuffixes = []struct {
	suffix     string
	multiplier int64
}{
	// Two-letter suffixes first, so that `Mi` is not read as `M` followed by garbage.
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30},
	{"k", 1e3}, {"M", 1e6}, {"G", 1e9},
}

// UnmarshalText reads a quantity, which is how the config decoder hands one over.
func (b *ByteSize) UnmarshalText(text []byte) error {
	s := string(text)
	number, multiplier := s, int64(1)
	for _, u := range byteSizeSuffixes {
		if strings.HasSuffix(s, u.suffix) {
			number, multiplier = strings.TrimSuffix(s, u.suffix), u.multiplier
			break
		}
	}
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil || n < 0 || strings.HasPrefix(number, "+") {
		return fmt.Errorf("invalid size %q: want a whole number of bytes, optionally with a Ki, Mi, Gi, k, M or G suffix", s)
	}
	if n > math.MaxInt64/multiplier {
		return fmt.Errorf("invalid size %q: too large", s)
	}
	*b = ByteSize(n * multiplier)
	return nil
}
