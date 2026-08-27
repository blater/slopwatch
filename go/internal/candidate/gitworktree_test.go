package candidate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/isolation"
)

const testCandidateCommandOutputBytes = int64(4 << 20)

func testGitWorktreeConfig() GitWorktreeConfig {
	return GitWorktreeConfig{DiscoveryCommandOutputBytes: testCandidateCommandOutputBytes}
}

func TestCandidateReadFileRejectsOversizedAgentOutput(t *testing.T) {
	repository := initializeRepository(t)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	const previewBytes = 4 << 20
	if err := os.WriteFile(filepath.Join(identity.RepositoryRoot, "main.go"), bytes.Repeat([]byte("x"), previewBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := service.ReadFile(context.Background(), identity, target, previewBytes)
	if err != nil || !preview.Truncated || len(preview.Contents) != previewBytes {
		t.Fatalf("ReadFile oversized preview=%+v error=%v", preview, err)
	}
}

func TestCandidateReadFileDoesNotFollowExternalSymlink(t *testing.T) {
	repository := initializeRepository(t)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(t.Context(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(identity.RepositoryRoot, target.String())); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(identity.RepositoryRoot, target.String())); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadFile(t.Context(), identity, target, 1024); err == nil || !strings.Contains(err.Error(), "not regular") {
		t.Fatalf("external symlink preview error = %v", err)
	}
}

func TestCandidateReconcilesDiscardAfterWorktreeRemovalCrash(t *testing.T) {
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
	if err := os.RemoveAll(identity.RepositoryRoot); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	if err := restarted.ReconcileDiscard(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, string(job))); !os.IsNotExist(err) {
		t.Fatalf("reconciled discard retained job root: %v", err)
	}
}

func TestCandidateReconcilesRegisteredWorktreeAfterWholeJobStateIsLost(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(t.Context(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(state, string(job))); err != nil {
		t.Fatal(err)
	}
	if listing := gitOutput(t, repository, "worktree", "list", "--porcelain"); !strings.Contains(listing, identity.RepositoryRoot) {
		t.Fatalf("test did not retain exact Git registration:\n%s", listing)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	if err := restarted.ReconcileDiscard(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	if listing := gitOutput(t, repository, "worktree", "list", "--porcelain"); strings.Contains(listing, identity.RepositoryRoot) {
		t.Fatalf("reconciled discard retained Git registration:\n%s", listing)
	}
}

func TestPrepareReusesExactOwnedCandidateAfterCrashBeforeStateSave(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	identity, err := first.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity.RepositoryRoot, "main.go"), []byte("owned mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	recovered, err := restarted.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != identity {
		t.Fatalf("Prepare returned different candidate: got %+v want %+v", recovered, identity)
	}
	contents, err := os.ReadFile(filepath.Join(recovered.RepositoryRoot, "main.go"))
	if err != nil || string(contents) != "owned mutation\n" {
		t.Fatalf("existing candidate was replaced or altered: %q, %v", contents, err)
	}
}

func TestPrepareCancellationCleansRegisteredWorktreeAndRemainsRetryable(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	ctx, cancel := context.WithCancel(context.Background())
	executor := &cancelAfterWorktreeAddExecutor{cancel: cancel}
	service, err := NewGitWorktreeService(state, executor, testGitWorktreeConfig())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.DiscoverWorkspace(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace,
		Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	if _, err := service.Prepare(ctx, request); err == nil {
		t.Fatal("Prepare unexpectedly succeeded after cancellation between worktree add and checkout")
	}
	worktree := filepath.Join(service.stateRoot, string(job), "worktree")
	if output := gitOutput(t, repository, "worktree", "list", "--porcelain"); strings.Contains(output, worktree) {
		t.Fatalf("canceled Prepare left registered worktree metadata:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(state, string(job))); !os.IsNotExist(err) {
		t.Fatalf("canceled Prepare retained state after exact cleanup: %v", err)
	}
	identity, err := service.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("retry after canceled Prepare: %v", err)
	}
	if identity.RepositoryRoot != worktree {
		t.Fatalf("retry worktree = %q, want %q", identity.RepositoryRoot, worktree)
	}
}

func TestPreparePromotesExactReservationAfterCrashBeforeOwnershipMarker(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	identity, err := first.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, string(job), ownershipName)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	recovered, err := restarted.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != identity {
		t.Fatalf("reservation promoted a different identity: got %+v want %+v", recovered, identity)
	}
	if _, err := os.Stat(filepath.Join(state, string(job), ownershipName)); err != nil {
		t.Fatalf("ownership marker was not promoted: %v", err)
	}
}

func TestPrepareRefusesAmbiguousUnmarkedWorktreeWithoutDeletingIt(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	identity, err := first.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	jobRoot := filepath.Join(state, string(job))
	if err := os.Remove(filepath.Join(jobRoot, ownershipName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(jobRoot, reservationName)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	if _, err := restarted.Prepare(context.Background(), request); err == nil {
		t.Fatal("ambiguous unmarked worktree was adopted")
	}
	if _, err := os.Stat(identity.RepositoryRoot); err != nil {
		t.Fatalf("ambiguous worktree was deleted: %v", err)
	}
}

func TestGitWorktreeServiceCreatesDetachedCandidatesAndEnforcesScope(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, err := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.DiscoverWorkspace(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	candidate, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	branch := gitOutput(t, candidate.RepositoryRoot, "symbolic-ref", "--short", "-q", "HEAD")
	if branch != "" {
		t.Fatalf("candidate is on branch %q, want detached HEAD", branch)
	}
	if err := os.WriteFile(filepath.Join(candidate.RepositoryRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err := service.Diff(context.Background(), candidate)
	if err != nil || diff.Scope != fix.ScopeClean || len(diff.Files) != 1 || diff.Files[0].Path != target {
		t.Fatalf("target diff = %+v, %v", diff, err)
	}
	if err := os.WriteFile(filepath.Join(candidate.RepositoryRoot, "surprise.txt"), []byte("oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err = service.Diff(context.Background(), candidate)
	if err != nil || diff.Scope != fix.ScopeClean || len(diff.Files) != 2 {
		t.Fatalf("supporting refactor diff = %+v, %v", diff, err)
	}
	if err := service.Discard(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate.RepositoryRoot); !os.IsNotExist(err) {
		t.Fatalf("discarded candidate still exists: %v", err)
	}
}

func TestCandidatePreservesAnalysisRootRelativeToRepository(t *testing.T) {
	repository := initializeRepository(t)
	analysis := filepath.Join(repository, "nested")
	if err := os.MkdirAll(analysis, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(analysis, "target.go"), []byte("package nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "nested/target.go")
	gitRun(t, repository, "commit", "-q", "-m", "nested")
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	workspace.AnalysisRoot = analysis
	target, _ := fix.ParseRepoPath("nested/target.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if identity.AnalysisRoot != filepath.Join(identity.RepositoryRoot, "nested") {
		t.Fatalf("analysis root=%q", identity.AnalysisRoot)
	}
}

func TestValidJobIDAcceptsGonamesOnly(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"job-calm-swift-otter", true},
		{"job-two-words", false},
		{"job-waytoolong-swift-otter", false},
		{"job-calm-swift-otter/escape", false},
		{"draft-calm-swift-otter", false},
	}
	for _, test := range tests {
		if got := validJobID(fix.JobID(test.value)); got != test.valid {
			t.Errorf("validJobID(%q) = %t, want %t", test.value, got, test.valid)
		}
	}
}

func TestGitWorktreeServiceSeedsAllowedCurrentChanges(t *testing.T) {
	repository := initializeRepository(t)
	service, err := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	const currentTarget = "package main\n\nfunc main() { println(\"current\") }\n"
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte(currentTarget), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "untracked"), []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	isolated, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(isolated.RepositoryRoot, "main.go"))
	if err != nil || string(contents) != currentTarget {
		t.Fatalf("isolated target = %q, err=%v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(isolated.RepositoryRoot, "untracked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unrelated current file entered isolated scope: %v", err)
	}
}

func TestCandidateSnapshotPreservesRenameCopyAndExecutableMode(t *testing.T) {
	repository := initializeRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "old.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "source.go"), []byte("package source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "old.go", "source.go")
	gitRun(t, repository, "commit", "-q", "-m", "seed files")
	gitRun(t, repository, "config", "status.renames", "copies")
	gitRun(t, repository, "mv", "old.go", "new.go")
	contents, _ := os.ReadFile(filepath.Join(repository, "source.go"))
	if err := os.WriteFile(filepath.Join(repository, "copy.go"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "copy.go")
	if err := os.Chmod(filepath.Join(repository, "main.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	job, _ := fix.NewJobID()
	target, _ := fix.ParseRepoPath("main.go")
	identity, err := service.Prepare(t.Context(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace,
		Targets: []fix.RepoPath{target}, AllowedScope: "repository"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(identity.RepositoryRoot, "old.go")); !os.IsNotExist(err) {
		t.Fatalf("rename source remains in candidate: %v", err)
	}
	for _, path := range []string{"new.go", "source.go", "copy.go"} {
		if _, err := os.Stat(filepath.Join(identity.RepositoryRoot, path)); err != nil {
			t.Fatalf("candidate omitted %s: %v", path, err)
		}
	}
	info, err := os.Stat(filepath.Join(identity.RepositoryRoot, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("candidate lost executable mode: mode=%v", info.Mode())
	}
}

func TestCandidateAppliesImmutableSnapshotAfterSourceChanges(t *testing.T) {
	repository := initializeRepository(t)
	const snapshotted = "package main\n// snapshotted\n"
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte(snapshotted), 0o600); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	staging := filepath.Join(state, "staging")
	destination := filepath.Join(state, "destination")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitWorktreeService(filepath.Join(state, "service"), directExecutor{}, testGitWorktreeConfig())
	target, _ := fix.ParseRepoPath("main.go")
	manifest, err := service.snapshotWorkingChanges(withCommandOutputBytes(t.Context(), testCandidateCommandOutputBytes), repository, staging, "targets", []fix.RepoPath{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n// later mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applySeedManifest(t.Context(), staging, destination, manifest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(destination, "main.go"))
	if string(got) != snapshotted {
		t.Fatalf("candidate used mutable source bytes: %q", got)
	}
}

func TestCandidateCleansAndRetriesPartialReservationWithoutCompletionMarker(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	identity, err := service.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	jobRoot := filepath.Join(state, string(job))
	if err := os.Remove(filepath.Join(jobRoot, ownershipName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(jobRoot, seedCompletedName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity.RepositoryRoot, "main.go"), []byte("partial crash state\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	recovered, err := restarted.Prepare(t.Context(), request)
	if err != nil {
		t.Fatalf("partial reservation was not cleaned and retried: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(recovered.RepositoryRoot, "main.go"))
	if err != nil || string(got) == "partial crash state\n" {
		t.Fatalf("partial reservation bytes were adopted: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(jobRoot, ownershipName)); err != nil {
		t.Fatalf("retried candidate has no ownership marker: %v", err)
	}
}

func TestCandidateRetriesReservationLeftBeforeWorktreeCreation(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
	identity, err := service.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(identity.RepositoryRoot); err != nil {
		t.Fatal(err)
	}
	if listing := gitOutput(t, repository, "worktree", "list", "--porcelain"); !strings.Contains(listing, identity.RepositoryRoot) {
		t.Fatalf("test did not leave the missing worktree registered:\n%s", listing)
	}
	jobRoot := filepath.Join(state, string(job))
	for _, marker := range []string{ownershipName, seedCompletedName} {
		if err := os.Remove(filepath.Join(jobRoot, marker)); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	recovered, err := restarted.Prepare(t.Context(), request)
	if err != nil {
		t.Fatalf("reservation without worktree was not retried: %v", err)
	}
	if _, err := os.Stat(recovered.RepositoryRoot); err != nil {
		t.Fatalf("retried worktree missing: %v", err)
	}
}

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
	identity, err := service.Prepare(context.Background(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, identity.RepositoryRoot, "mv", "main.go", "renamed.go")
	first, err := service.Diff(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 || first.Files[0].Path != "renamed.go" || first.Files[0].Previous != "main.go" {
		t.Fatalf("rename inventory = %+v", first.Files)
	}
	path := filepath.Join(identity.RepositoryRoot, "renamed.go")
	if err := os.WriteFile(path, []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, _ := service.Diff(context.Background(), identity)
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content was absent from candidate fingerprint")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	third, _ := service.Diff(context.Background(), identity)
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("Git executable mode was absent from candidate fingerprint")
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

type directExecutor struct{}

func (directExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = request.Environment
	command.Stdin = nil
	command.Stdin = bytes.NewReader(request.Stdin)
	stdout, err := command.Output()
	result := isolation.Result{Stdout: stdout}
	if err == nil {
		return result, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
		result.Stderr = exit.Stderr
		return result, nil
	}
	return result, err
}

type cancelAfterWorktreeAddExecutor struct {
	once   sync.Once
	cancel context.CancelFunc
}

func (executor *cancelAfterWorktreeAddExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	result, err := (directExecutor{}).Run(ctx, request)
	for index := 0; err == nil && result.Successful() && index+1 < len(request.Arguments); index++ {
		if request.Arguments[index] == "worktree" && request.Arguments[index+1] == "add" {
			executor.once.Do(executor.cancel)
			break
		}
	}
	return result, err
}

func initializeRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init", "-q")
	gitRun(t, repository, "config", "user.name", "Test")
	gitRun(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "main.go")
	gitRun(t, repository, "commit", "-q", "-m", "base")
	return repository
}

func gitRun(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func gitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, _ := command.Output()
	return string(output)
}
