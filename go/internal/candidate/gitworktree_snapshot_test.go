package candidate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

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
	manifest, err := service.executor.snapshotWorkingChanges(withCommandOutputBytes(t.Context(), testCandidateCommandOutputBytes), repository, staging, "targets", []fix.RepoPath{target})
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
