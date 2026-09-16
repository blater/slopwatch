package candidate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func worktreeScopeFixture(t *testing.T) (*GitWorktreeService, string, fix.WorkspaceIdentity, fix.RepoPath) {
	t.Helper()
	repository := initializeRepository(t)
	service, err := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.DiscoverWorkspace(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	target, err := fix.ParseRepoPath("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, workspace, target
}

func prepareScopeCandidate(t *testing.T, service *GitWorktreeService, workspace fix.WorkspaceIdentity, target fix.RepoPath) fix.CandidateIdentity {
	t.Helper()
	job, err := fix.NewJobID()
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func assertDetachedCandidate(t *testing.T, candidate fix.CandidateIdentity) {
	t.Helper()
	if branch := gitOutput(t, candidate.RepositoryRoot, "symbolic-ref", "--short", "-q", "HEAD"); branch != "" {
		t.Fatalf("candidate is on branch %q, want detached HEAD", branch)
	}
}

func assertScopeDiff(t *testing.T, service *GitWorktreeService, candidate fix.CandidateIdentity, want int, target fix.RepoPath, label string) {
	t.Helper()
	diff, err := service.Diff(context.Background(), candidate)
	if err != nil || diff.Scope != fix.ScopeClean || len(diff.Files) != want || (target != "" && diff.Files[0].Path != target) {
		t.Fatalf("%s diff = %+v, %v", label, diff, err)
	}
}

func writeCandidatePath(t *testing.T, candidate fix.CandidateIdentity, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(candidate.RepositoryRoot, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
