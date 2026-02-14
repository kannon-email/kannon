package stats

import "testing"

func TestInMemRepository(t *testing.T) {
	repo := NewInMemRepository()
	RunRepoSpec(t, repo)
}
