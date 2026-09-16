package candidate

import (
	"context"
	"os"
	"testing"
)

func TestGitWorktreeServiceCreatesDetachedCandidatesAndEnforcesScope(t *testing.T) {
	service, _, workspace, target := worktreeScopeFixture(t)
	candidate := prepareScopeCandidate(t, service, workspace, target)
	assertDetachedCandidate(t, candidate)
	writeCandidatePath(t, candidate, "main.go", "package main\n\nfunc main() {}\n")
	assertScopeDiff(t, service, candidate, 1, target, "target")
	writeCandidatePath(t, candidate, "surprise.txt", "oops")
	assertScopeDiff(t, service, candidate, 2, "", "supporting refactor")
	if err := service.Discard(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate.RepositoryRoot); !os.IsNotExist(err) {
		t.Fatalf("discarded candidate still exists: %v", err)
	}
}
