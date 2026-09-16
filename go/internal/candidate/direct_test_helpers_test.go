package candidate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func directFixture(t *testing.T) (string, string, string) {
	t.Helper()
	workspace := t.TempDir()
	allowed := filepath.Join(workspace, "allowed.go")
	other := filepath.Join(workspace, "other.go")
	writeDirectFile(t, allowed, "package before\n")
	writeDirectFile(t, other, "package other\n")
	return workspace, allowed, other
}

func writeDirectFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareDirectFixture(t *testing.T, service *DirectService, workspace string) fix.CandidateIdentity {
	t.Helper()
	job, err := fix.NewJobID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := service.Prepare(t.Context(), PrepareRequest{Job: job, Mode: fix.WorkspaceCurrent,
		Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: workspace, AnalysisRoot: workspace},
		Targets:   []fix.RepoPath{"allowed.go"}, AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{"allowed.go"}})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func assertDirectDiff(t *testing.T, diff DiffSnapshot) {
	t.Helper()
	if len(diff.Files) != 2 || diff.Scope != fix.ScopeClean || diff.Fingerprint == "" {
		t.Fatalf("direct diff = %+v", diff)
	}
}

func assertDirectFileUnchangedByDiscard(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "package fixed\n" {
		t.Fatalf("finishing direct candidate changed user files: %q, %v", contents, err)
	}
}
