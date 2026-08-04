package audit

import "testing"

func TestInMemRepository(t *testing.T) {
	repo := NewInMemRepository()
	RunRepoSpec(t, repo, repo.Records)
}
