package config

import "testing"

func TestOr_TellsAnUnsetKeyFromAZeroValue(t *testing.T) {
	zero := uint(0)
	if got := Or(&zero, 8080); got != 0 {
		t.Fatalf("Or(&0, 8080) = %d, want 0", got)
	}
	if got := Or[uint](nil, 8080); got != 8080 {
		t.Fatalf("Or(nil, 8080) = %d, want 8080", got)
	}
}
