package statssec

import "testing"

func TestInMemKeyRepository(t *testing.T) {
	RunKeyRepoSpec(t, NewInMemKeyRepository())
}
