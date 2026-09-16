package candidate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

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
