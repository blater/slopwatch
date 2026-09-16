package candidate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestCandidateSnapshotPreservesRenameCopyAndExecutableMode(t *testing.T) {
	repository := initializeRepository(t)
	prepareRenameCopyWorkspace(t, repository)
	identity := prepareRepositoryCandidate(t, repository)
	assertRenameCopyFiles(t, identity.RepositoryRoot)
	assertExecutableMode(t, identity.RepositoryRoot)
}

func prepareRenameCopyWorkspace(t *testing.T, repository string) {
	t.Helper()
	writeCandidateFile(t, repository, "old.go", "package old\n", 0o644)
	writeCandidateFile(t, repository, "source.go", "package source\n", 0o644)
	gitRun(t, repository, "add", "old.go", "source.go")
	gitRun(t, repository, "commit", "-q", "-m", "seed files")
	gitRun(t, repository, "config", "status.renames", "copies")
	gitRun(t, repository, "mv", "old.go", "new.go")
	contents, err := os.ReadFile(filepath.Join(repository, "source.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeCandidateFile(t, repository, "copy.go", string(contents), 0o644)
	gitRun(t, repository, "add", "copy.go")
	if err := os.Chmod(filepath.Join(repository, "main.go"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeCandidateFile(t *testing.T, repository, name, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func prepareRepositoryCandidate(t *testing.T, repository string) fix.CandidateIdentity {
	t.Helper()
	service, _ := NewGitWorktreeService(filepath.Join(t.TempDir(), "state"), directExecutor{}, testGitWorktreeConfig())
	workspace, _ := service.DiscoverWorkspace(t.Context(), repository)
	job, _ := fix.NewJobID()
	target, _ := fix.ParseRepoPath("main.go")
	identity, err := service.Prepare(t.Context(), PrepareRequest{CommandOutputBytes: testCandidateCommandOutputBytes, Job: job, Workspace: workspace,
		Targets: []fix.RepoPath{target}, AllowedScope: "repository"})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func assertRenameCopyFiles(t *testing.T, candidateRoot string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(candidateRoot, "old.go")); !os.IsNotExist(err) {
		t.Fatalf("rename source remains in candidate: %v", err)
	}
	for _, path := range []string{"new.go", "source.go", "copy.go"} {
		if _, err := os.Stat(filepath.Join(candidateRoot, path)); err != nil {
			t.Fatalf("candidate omitted %s: %v", path, err)
		}
	}
}

func assertExecutableMode(t *testing.T, candidateRoot string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(candidateRoot, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("candidate lost executable mode: mode=%v", info.Mode())
	}
}
