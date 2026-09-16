package delivery

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestCurrentBranchCommitKeepsUnrelatedStagedChanges(t *testing.T) {
	fixture := currentBranchFixture(t)
	service := mustTestGitService(t, testExecutor{})
	result := mustCurrentBranchCommit(t, service, fixture)
	assertCurrentBranchCommit(t, fixture.repository, fixture.branch, result)
}

type currentBranchCommitFixture struct {
	repository string
	base       string
	branch     string
	common     string
}

func currentBranchFixture(t *testing.T) currentBranchCommitFixture {
	t.Helper()
	repository, _ := deliveryRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "unrelated.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "add", "unrelated.txt")
	gitCommand(t, repository, "commit", "-q", "-m", "add unrelated")
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	branch := strings.TrimSpace(gitResult(t, repository, "symbolic-ref", "--short", "HEAD"))
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	if err := os.WriteFile(filepath.Join(repository, "unrelated.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "add", "unrelated.txt")
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package fixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return currentBranchCommitFixture{repository: repository, base: base, branch: branch, common: common}
}

func mustTestGitService(t *testing.T, executor isolation.Executor) *GitService {
	t.Helper()
	service, err := NewGitService(executor)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func mustCurrentBranchCommit(t *testing.T, service *GitService, fixture currentBranchCommitFixture) Result {
	t.Helper()
	plan := fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitCommitCurrent, Publish: fix.PublishLocal}
	result, err := service.CreateCommit(t.Context(), Request{Job: "job-current", Plan: plan, Paths: []fix.RepoPath{"main.go"}, DiffHash: "verified",
		Candidate: fix.CandidateIdentity{Job: "job-current", WorkspaceMode: fix.WorkspaceCurrent, RepositoryRoot: fixture.repository, GitCommonDir: fixture.common, BaseCommit: fix.ObjectID(fixture.base)},
		Branch:    fixture.branch, CommitTitle: "Fix main", CommandOutputBytes: testDeliveryOutputBytes})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertCurrentBranchCommit(t *testing.T, repository, branch string, result Result) {
	t.Helper()
	if result.Commit == "" || result.LocalRef != "refs/heads/"+branch {
		t.Fatalf("current branch result = %+v", result)
	}
	if names := gitResult(t, repository, "show", "--format=", "--name-only", "HEAD"); !strings.Contains(names, "main.go") || strings.Contains(names, "unrelated.txt") {
		t.Fatalf("current branch commit files = %q", names)
	}
	if staged := gitResult(t, repository, "diff", "--cached", "--name-only"); !strings.Contains(staged, "unrelated.txt") || strings.Contains(staged, "main.go") {
		t.Fatalf("staged files after current branch commit = %q", staged)
	}
}

func TestCanceledLocalRefCreationIsReconciledExactly(t *testing.T) {
	fixture := canceledLocalRefFixture(t)
	ambiguous, err := fixture.canceling.CreateLocalRef(t.Context(), fixture.request, fixture.committed)
	assertCanceledLocalRef(t, ambiguous, err)
	reconciled, err := fixture.normal.Reconcile(t.Context(), fixture.request, ambiguous)
	assertReconciledLocalRef(t, reconciled, err)
}

type canceledLocalRefTestFixture struct {
	normal, canceling *GitService
	request           Request
	committed         Result
}

func canceledLocalRefFixture(t *testing.T) canceledLocalRefTestFixture {
	t.Helper()
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	normal := mustTestGitService(t, testExecutor{})
	fingerprint, err := normal.diffFingerprint(withCommandOutput(t.Context(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := remoteIdentity(remote)
	request := Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)},
		DiffHash: fingerprint, Plan: pushNewBranch, Paths: []fix.RepoPath{"main.go"}, Branch: "slopwatch/fix/canceled-local", Remote: "origin", CommitTitle: "Refactor main.go", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes}
	committed, err := normal.CreateCommit(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	canceledRunner := executorFunc(func(ctx context.Context, run isolation.Request) (isolation.Result, error) {
		result, err := (testExecutor{}).Run(ctx, run)
		if slices.Contains(run.Arguments, "update-ref") && err == nil && result.ExitCode == 0 {
			result.Canceled = true
		}
		return result, err
	})
	canceling := mustTestGitService(t, canceledRunner)
	return canceledLocalRefTestFixture{normal: normal, canceling: canceling, request: request, committed: committed}
}

func assertCanceledLocalRef(t *testing.T, ambiguous Result, err error) {
	t.Helper()
	if err == nil || !ambiguous.Ambiguous || ambiguous.LocalRef != "" {
		t.Fatalf("canceled local ref result = %+v, %v", ambiguous, err)
	}
}

func assertReconciledLocalRef(t *testing.T, reconciled Result, err error) {
	t.Helper()
	if err != nil || reconciled.Ambiguous || reconciled.LocalRef != "refs/heads/slopwatch/fix/canceled-local" {
		t.Fatalf("local ref reconciliation = %+v, %v", reconciled, err)
	}
}
