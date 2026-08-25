package candidate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestCandidateReadFileRejectsOversizedAgentOutput(t *testing.T) {
	repository := initializeRepository(t)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity.RepositoryRoot, "main.go"), bytes.Repeat([]byte("x"), MaxReadFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadFile(context.Background(), identity, target); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("ReadFile oversized error = %v", err)
	}
}

func TestCandidateReconcilesDiscardAfterWorktreeRemovalCrash(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "worktree", "remove", "--force", identity.RepositoryRoot)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{})
	if err := restarted.ReconcileDiscard(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, string(job))); !os.IsNotExist(err) {
		t.Fatalf("reconciled discard retained job root: %v", err)
	}
}

func TestPrepareReusesExactOwnedCandidateAfterCrashBeforeJournalHandshake(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
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
	restarted, _ := NewGitWorktreeService(state, directExecutor{})
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

func TestPreparePromotesExactReservationAfterCrashBeforeOwnershipMarker(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	first, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
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
	restarted, _ := NewGitWorktreeService(state, directExecutor{})
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
	first, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	request := PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets", AllowedPaths: []fix.RepoPath{target}}
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
	restarted, _ := NewGitWorktreeService(state, directExecutor{})
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
	service, err := NewGitWorktreeService(state, directExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.DiscoverWorkspace(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	preflight, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Targets: []fix.RepoPath{target}})
	if err != nil || !preflight.Clean || !preflight.Supported || preflight.TargetBlobs[target] == "" {
		t.Fatalf("preflight = %+v, %v", preflight, err)
	}
	job, _ := fix.NewJobID()
	candidate, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
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
	if err != nil || diff.Scope != fix.ScopeViolated {
		t.Fatalf("out-of-scope diff = %+v, %v", diff, err)
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
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	workspace.AnalysisRoot = analysis
	target, _ := fix.ParseRepoPath("nested/target.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if identity.AnalysisRoot != filepath.Join(identity.RepositoryRoot, "nested") {
		t.Fatalf("analysis root=%q", identity.AnalysisRoot)
	}
}

func TestGitWorktreeServiceRejectsDirtyRepository(t *testing.T) {
	repository := initializeRepository(t)
	service, err := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	if err := os.WriteFile(filepath.Join(repository, "untracked"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	preflight, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Targets: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Clean || preflight.Supported {
		t.Fatalf("dirty preflight = %+v", preflight)
	}
}

func TestGitWorktreeServiceRejectsInProgressRepositoryOperation(t *testing.T) {
	repository := initializeRepository(t)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	if err := os.Mkdir(filepath.Join(repository, ".git", "rebase-merge"), 0o700); err != nil {
		t.Fatal(err)
	}
	target, _ := fix.ParseRepoPath("main.go")
	preflight, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Targets: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Supported || preflight.Diagnostic == "" {
		t.Fatalf("in-progress operation accepted: %+v", preflight)
	}
}

func TestCandidateManifestCoversBytesModeAndRenameEndpoints(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
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
	service, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{})
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
	first, _ := NewGitWorktreeService(state, directExecutor{})
	second, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "other-state"), directExecutor{})
	workspace, _ := first.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	jobOne, _ := fix.NewJobID()
	identity, err := first.Prepare(context.Background(), PrepareRequest{Job: jobOne, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
	if err != nil {
		t.Fatal(err)
	}
	jobTwo, _ := fix.NewJobID()
	if _, err := second.Prepare(context.Background(), PrepareRequest{Job: jobTwo, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"}); err == nil {
		t.Fatal("second process/service acquired a repository with a retained candidate")
	}
	if err := first.Discard(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if candidate, err := second.Prepare(context.Background(), PrepareRequest{Job: jobTwo, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"}); err != nil {
		t.Fatalf("lease was not released after discard: %v", err)
	} else {
		_ = second.Discard(context.Background(), candidate)
	}
}

func TestCandidateRejectsSymlinkOwnershipMarker(t *testing.T) {
	repository := initializeRepository(t)
	state := filepath.Join(t.TempDir(), "state")
	service, _ := NewGitWorktreeService(state, directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(context.Background(), PrepareRequest{Job: job, Workspace: workspace, Targets: []fix.RepoPath{target}, AllowedScope: "targets"})
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

func TestPreflightRejectsAttributesOutsideRepositoryRootFileAndDisablesFsmonitor(t *testing.T) {
	repository := initializeRepository(t)
	if err := os.MkdirAll(filepath.Join(repository, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "nested", ".gitattributes"), []byte("future-*.txt working-tree-encoding=UTF-16\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "nested", "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "nested")
	gitRun(t, repository, "commit", "-q", "-m", "attributes")
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	hook := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "config", "core.fsmonitor", hook)
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	preflight, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Targets: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Supported || preflight.Diagnostic == "" {
		t.Fatalf("content-transforming nested attributes accepted: %+v", preflight)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured fsmonitor executed: %v", err)
	}
}

func TestPreflightRejectsGitInfoAttributesForFuturePaths(t *testing.T) {
	repository := initializeRepository(t)
	if err := os.MkdirAll(filepath.Join(repository, ".git", "info"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git", "info", "attributes"), []byte("future-* filter=unsafe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{})
	workspace, _ := service.DiscoverWorkspace(context.Background(), repository)
	target, _ := fix.ParseRepoPath("main.go")
	preflight, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Targets: []fix.RepoPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Supported {
		t.Fatalf("info/attributes content transform accepted: %+v", preflight)
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
