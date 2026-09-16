package candidate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

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
