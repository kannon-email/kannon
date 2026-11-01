package domains

import "testing"

func TestInMemRepository(t *testing.T) {
	RunRepoSpec(t, NewInMemRepository())
}
