package candidate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestGitWorktreeServiceAttemptsCandidateDuringRepositoryOperation(t *testing.T) {
	repository := initializeRepository(t)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	if err := os.Mkdir(filepath.Join(repository, ".git", "rebase-merge"), 0o700); err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	if _, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{target}}); err != nil {
		t.Fatalf("candidate operation was rejected by speculative repository policy: %v", err)
	}
}

func TestCandidateManifestCoversBytesModeAndRenameEndpoints(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity := prepareLifecycleCandidate(t, service, job, workspace, target)
	gitRun(t, identity.RepositoryRoot, "mv", "main.go", "renamed.go")
	first := diffLifecycleCandidate(t, service, identity)
	assertRenameInventory(t, first)
	path := filepath.Join(identity.RepositoryRoot, "renamed.go")
	writeCandidateFile(t, identity.RepositoryRoot, "renamed.go", "package changed\n", 0o644)
	second := diffLifecycleCandidate(t, service, identity)
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content was absent from candidate fingerprint")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	third := diffLifecycleCandidate(t, service, identity)
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("Git executable mode was absent from candidate fingerprint")
	}
}

func prepareLifecycleCandidate(t *testing.T, service *GitWorktreeService, job fix.JobID, workspace fix.WorkspaceIdentity, target fix.RepoPath) fix.CandidateIdentity {
	t.Helper()
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func diffLifecycleCandidate(t *testing.T, service *GitWorktreeService, identity fix.CandidateIdentity) DiffSnapshot {
	t.Helper()
	diff, err := service.Diff(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	return diff
}

func assertRenameInventory(t *testing.T, diff DiffSnapshot) {
	t.Helper()
	if len(diff.Files) != 1 || diff.Files[0].Path != "renamed.go" || diff.Files[0].Previous != "main.go" {
		t.Fatalf("rename inventory = %+v", diff.Files)
	}
}

func TestCandidateRecoveryUsesDurableOwnershipAndRejectsForgery(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	if err := restarted.Recover(context.Background(), identity, []fix.RepoPath{target}, "targets", []fix.RepoPath{target}); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if _, err := restarted.Diff(context.Background(), identity); err != nil {
		t.Fatalf("diff after recovery: %v", err)
	}
	forged := identity
	forged.RepositoryRoot = filepath.Join(state, string(job), "sibling")
	forged.AnalysisRoot = forged.RepositoryRoot
	if _, err := restarted.Diff(context.Background(), forged); err == nil {
		t.Fatal("forged sibling candidate was accepted")
	}
	tampered := identity
	tampered.BaseCommit = "deadbeef"
	if _, err := restarted.Diff(context.Background(), tampered); err == nil {
		t.Fatal("tampered ownership tuple was accepted")
	}
}

func TestRepositoryOwnershipLeaseSpansCandidateLifetime(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	second, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "other-state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	jobOne, _ := fix.NewJobID()
	identity, err := first.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: jobOne, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	jobTwo, _ := fix.NewJobID()
	if _, err := second.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: jobTwo, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"}); err == nil {
		t.Fatal("second process/service acquired a repository with a retained candidate")
	}
	if err := first.Discard(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if candidate, err := second.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: jobTwo, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"}); err != nil {
		t.Fatalf("lease was not released after discard: %v", err)
	} else {
		_ = second.Discard(context.Background(), candidate)
	}
}

func TestCandidateRejectsSymlinkOwnershipMarker(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(state, string(job), ownershipName)
	copyPath := filepath.Join(t.TempDir(), "owner-copy")
	data, _ := os.ReadFile(marker)
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(copyPath, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Diff(context.Background(), identity); err == nil {
		t.Fatal("symlink ownership marker was accepted")
	}
}
