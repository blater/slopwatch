package candidate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestCandidateAppliesImmutableSnapshotAfterSourceChanges(t *testing.T) {
	fixture := newSnapshotFixture(t)
	manifest := fixture.snapshot(t)
	writeCandidateFile(t, fixture.repository, "main.go", "package main\n// later mutation\n", 0o600)
	if err := applySeedManifest(t.Context(), fixture.staging, fixture.destination, manifest); err != nil {
		t.Fatal(err)
	}
	assertSnapshotBytes(t, fixture.destination, "package main\n// snapshotted\n")
}

type snapshotFixture struct {
	repository, staging, destination string
	service                          *GitWorktreeService
}

func newSnapshotFixture(t *testing.T) snapshotFixture {
	t.Helper()
	repository := initializeRepository(t)
	writeCandidateFile(t, repository, "main.go", "package main\n// snapshotted\n", 0o600)
	state := t.TempDir()
	staging := filepath.Join(state, "staging")
	destination := filepath.Join(state, "destination")
	makeCandidateDirectory(t, staging)
	makeCandidateDirectory(t, destination)
	service, _ := NewGitWorktreeService(filepath.Join(state, "service"), directExecutor{}, testGitWorktreeConfig())
	return snapshotFixture{repository: repository, staging: staging, destination: destination, service: service}
}

func makeCandidateDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func (fixture snapshotFixture) snapshot(t *testing.T) seedManifest {
	t.Helper()
	target, _ := fix.ParseRepoPath("main.go")
	manifest, err := fixture.service.executor.snapshotWorkingChanges(withCommandOutputBytes(t.Context(), testCandidateCommandOutputBytes), fixture.repository, fixture.staging, "targets", []fix.RepoPath{target})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertSnapshotBytes(t *testing.T, destination, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(destination, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
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
	preparePartialReservation(t, service, request, state)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewGitWorktreeService(state, directExecutor{}, testGitWorktreeConfig())
	recovered, err := restarted.Prepare(t.Context(), request)
	if err != nil {
		t.Fatalf("partial reservation was not cleaned and retried: %v", err)
	}
	assertRetriedReservation(t, recovered.RepositoryRoot, filepath.Join(state, string(job)))
}

func preparePartialReservation(t *testing.T, service *GitWorktreeService, request PrepareRequest, state string) {
	t.Helper()
	identity, err := service.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	jobRoot := filepath.Join(state, string(request.Job))
	removeCandidateMarker(t, filepath.Join(jobRoot, ownershipName))
	removeCandidateMarker(t, filepath.Join(jobRoot, seedCompletedName))
	writeCandidateFile(t, identity.RepositoryRoot, "main.go", "partial crash state\n", 0o600)
}

func removeCandidateMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func assertRetriedReservation(t *testing.T, candidateRoot, jobRoot string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(candidateRoot, "main.go"))
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
