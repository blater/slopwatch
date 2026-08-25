package delivery

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestGitServiceCreatesExactAbsentLocalAndRemoteRefs(t *testing.T) {
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewGitService(testExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := service.diffFingerprint(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PublishCommit(context.Background(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)},
		DiffHash: fingerprint, Branch: "slopwatch/fix/main-test", Remote: "origin", CommitTitle: "Refactor main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pushed || result.Commit == "" || result.LocalRef != "refs/heads/slopwatch/fix/main-test" {
		t.Fatalf("delivery result = %+v", result)
	}
	if head := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD")); head != base {
		t.Fatalf("publication moved candidate HEAD to %s, want pinned base %s", head, base)
	}
	if status := gitResult(t, repository, "status", "--porcelain=v1"); !strings.Contains(status, "main.go") {
		t.Fatalf("publication mutated candidate index/status: %q", status)
	}
	published := gitResult(t, repository, "show", string(result.Commit)+":main.go")
	if !strings.Contains(published, "func main") {
		t.Fatalf("published commit omitted candidate bytes: %q", published)
	}
	remoteOID := strings.Fields(gitResult(t, repository, "ls-remote", remote, result.RemoteRef))[0]
	if remoteOID != string(result.Commit) {
		t.Fatalf("remote oid = %s, want %s", remoteOID, result.Commit)
	}
	if _, err := service.PublishCommit(context.Background(), Request{Job: "job-other", Candidate: fix.CandidateIdentity{Job: "job-other", RepositoryRoot: repository, GitCommonDir: common},
		DiffHash: fingerprint, Branch: "slopwatch/fix/main-test", Remote: "origin", CommitTitle: "Again"}); err == nil {
		t.Fatal("reused existing branch")
	}
}

func TestPullRequestPreflightRequiresResolvableBase(t *testing.T) {
	repository, _ := deliveryRepository(t)
	service, _ := NewGitService(testExecutor{})
	workspace := fix.WorkspaceIdentity{RepositoryRoot: repository}
	for _, base := range []string{"", "does-not-exist"} {
		err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Mode: fix.DeliveryModePullRequest, Remote: "origin", BaseBranch: base, Branch: "slopwatch/fix/test"})
		if err == nil {
			t.Fatalf("base %q accepted", base)
		}
	}
	if err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Mode: fix.DeliveryModePullRequest, Remote: "origin", BaseBranch: "HEAD", Branch: "slopwatch/fix/test"}); err != nil {
		t.Fatal(err)
	}
}

func TestGitServiceRejectsAttributesAddedAfterPreflight(t *testing.T) {
	repository, _ := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".gitattributes"), []byte("main.go filter=late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitService(testExecutor{})
	fingerprint, err := service.diffFingerprint(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateCommit(context.Background(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)}, DiffHash: fingerprint, Branch: "slopwatch/fix/attrs", Remote: "origin"})
	if err == nil || !strings.Contains(err.Error(), "content-transforming") {
		t.Fatalf("late transforming attribute accepted: %v", err)
	}
	if head := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD")); head != base {
		t.Fatalf("rejected publication moved HEAD: %s", head)
	}
}

type testExecutor struct{}

func (testExecutor) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = request.Environment
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

func deliveryRepository(t *testing.T) (string, string) {
	t.Helper()
	repository := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitCommand(t, repository, "init", "-q")
	gitCommand(t, repository, "config", "user.name", "Test")
	gitCommand(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "add", "main.go")
	gitCommand(t, repository, "commit", "-q", "-m", "base")
	gitCommand(t, repository, "init", "-q", "--bare", remote)
	gitCommand(t, repository, "remote", "add", "origin", remote)
	return repository, remote
}

func gitCommand(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func gitResult(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return string(output)
}
