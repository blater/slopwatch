package candidate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestDirectCandidateAllowsSupportingRefactorsAndNeverRollsBackCurrentFiles(t *testing.T) {
	workspace := t.TempDir()
	allowed := filepath.Join(workspace, "allowed.go")
	other := filepath.Join(workspace, "other.go")
	if err := os.WriteFile(allowed, []byte("package before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("package other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewDirectService(filepath.Join(t.TempDir(), "current"))
	if err != nil {
		t.Fatal(err)
	}
	job, _ := fix.NewJobID()
	identity, err := service.Prepare(t.Context(), PrepareRequest{Job: job, Mode: fix.WorkspaceCurrent,
		Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: workspace, AnalysisRoot: workspace},
		Targets:   []fix.RepoPath{"allowed.go"}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{"allowed.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowed, []byte("package fixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("package changed_elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err := service.Diff(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Files) != 2 || diff.Scope != fix.ScopeClean || diff.Fingerprint == "" {
		t.Fatalf("direct diff = %+v", diff)
	}
	if err := service.Discard(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(allowed)
	if err != nil || string(contents) != "package fixed\n" {
		t.Fatalf("finishing direct candidate changed user files: %q, %v", contents, err)
	}
}
