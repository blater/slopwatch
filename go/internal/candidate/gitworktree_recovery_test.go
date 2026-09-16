package candidate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

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
