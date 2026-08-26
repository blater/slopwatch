package delivery

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

const testDeliveryOutputBytes = int64(4 << 20)

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
	fingerprint, err := service.diffFingerprint(withCommandOutput(context.Background(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteIdentity(remote)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PublishCommit(context.Background(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)},
		DiffHash: fingerprint, Branch: "slopwatch/fix/main-test", Remote: "origin", CommitTitle: "Refactor main.go", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes})
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
		DiffHash: fingerprint, Branch: "slopwatch/fix/main-test", Remote: "origin", CommitTitle: "Again", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes}); err == nil {
		t.Fatal("reused existing branch")
	}
}

func TestCanceledLocalRefCreationIsReconciledExactly(t *testing.T) {
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	normal, _ := NewGitService(testExecutor{})
	fingerprint, err := normal.diffFingerprint(withCommandOutput(t.Context(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := remoteIdentity(remote)
	request := Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)},
		DiffHash: fingerprint, Branch: "slopwatch/fix/canceled-local", Remote: "origin", CommitTitle: "Refactor main.go", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes}
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
	canceling, _ := NewGitService(canceledRunner)
	ambiguous, err := canceling.CreateLocalRef(t.Context(), request, committed)
	if err == nil || !ambiguous.Ambiguous || ambiguous.LocalRef != "" {
		t.Fatalf("canceled local ref result = %+v, %v", ambiguous, err)
	}
	reconciled, err := normal.Reconcile(t.Context(), request, ambiguous)
	if err != nil || reconciled.Ambiguous || reconciled.LocalRef != "refs/heads/slopwatch/fix/canceled-local" {
		t.Fatalf("local ref reconciliation = %+v, %v", reconciled, err)
	}
}

func TestPullRequestPreflightRequiresResolvableBase(t *testing.T) {
	repository, _ := deliveryRepository(t)
	service, _ := NewGitService(testExecutor{})
	workspace := fix.WorkspaceIdentity{RepositoryRoot: repository}
	for _, base := range []string{"", "does-not-exist"} {
		_, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Mode: fix.DeliveryModePullRequest, Remote: "origin", BaseBranch: base, Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes})
		if err == nil {
			t.Fatalf("base %q accepted", base)
		}
	}
	if _, err := service.Preflight(context.Background(), PreflightRequest{Workspace: workspace, Mode: fix.DeliveryModePullRequest, Remote: "origin", BaseBranch: "HEAD", Branch: "slopwatch/fix/test", CommandOutputBytes: testDeliveryOutputBytes}); err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("local remote accepted for pull request: %v", err)
	}
}

func TestPullRequestPreflightRequiresLiteralBaseOnAdmittedRemote(t *testing.T) {
	for _, test := range []struct {
		name       string
		base       string
		remoteBase bool
		wantError  string
	}{
		{name: "local-only base", base: "main", wantError: "does not exist on the admitted remote"},
		{name: "revision expression", base: "main~1", remoteBase: true, wantError: "not a literal branch"},
		{name: "remote branch", base: "main", remoteBase: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &GitService{git: "/usr/bin/git", runner: remoteBasePreflightExecutor(test.remoteBase)}
			result, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: "/repo"},
				Mode: fix.DeliveryModePullRequest, Remote: "origin", BaseBranch: test.base, Branch: "slopwatch/fix/test", Publication: true, CommandOutputBytes: testDeliveryOutputBytes})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Preflight() error = %v", err)
				}
				return
			}
			if err != nil || result.RemoteHost != "github.com" || result.HostRepository != "owner/repo" || !strings.HasPrefix(result.RemoteIdentity, "sha256:") {
				t.Fatalf("Preflight() = %+v, %v", result, err)
			}
		})
	}
}

func remoteBasePreflightExecutor(baseExists bool) executorFunc {
	return func(_ context.Context, request isolation.Request) (isolation.Result, error) {
		arguments := request.Arguments
		last := ""
		if len(arguments) > 0 {
			last = arguments[len(arguments)-1]
		}
		joined := strings.Join(arguments, "\x00")
		switch {
		case strings.Contains(joined, "remote\x00get-url\x00--push\x00origin"):
			return isolation.Result{Stdout: []byte("https://github.com/owner/repo.git\n")}, nil
		case strings.Contains(joined, "check-ref-format\x00--branch"):
			if strings.ContainsAny(last, "~^:") {
				return isolation.Result{ExitCode: 128, Stderr: []byte("invalid branch")}, nil
			}
			return isolation.Result{Stdout: []byte(last + "\n")}, nil
		case strings.Contains(joined, "rev-parse\x00--verify\x00--quiet"):
			return isolation.Result{ExitCode: 1}, nil
		case strings.Contains(joined, "ls-remote\x00--exit-code\x00--refs"):
			if last == "refs/heads/main" && baseExists {
				return isolation.Result{Stdout: []byte(strings.Repeat("a", 40) + "\t" + last + "\n")}, nil
			}
			return isolation.Result{ExitCode: 1}, nil
		default:
			return isolation.Result{ExitCode: 128, Stderr: []byte("unexpected git command")}, nil
		}
	}
}

func TestDeliveryRejectsOptionLikeRemoteAliasBeforeGitExecution(t *testing.T) {
	service := &GitService{git: "/usr/bin/git", runner: executorFunc(func(context.Context, isolation.Request) (isolation.Result, error) {
		t.Fatal("unsafe remote alias reached Git")
		return isolation.Result{}, nil
	})}
	_, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: "/repo"}, Mode: fix.DeliveryModeBranch,
		Remote: "--upload-pack=touch-pwn", Branch: "slopwatch/fix/test"})
	if err == nil || !strings.Contains(err.Error(), "safe configured remote alias") {
		t.Fatalf("unsafe remote alias error=%v", err)
	}
}

func TestDeliveryRejectsCredentialBearingAndUnsupportedRemoteURLsWithoutEcho(t *testing.T) {
	repository, _ := deliveryRepository(t)
	service, _ := NewGitService(testExecutor{})
	for _, test := range []struct {
		name, remoteURL, secret string
	}{
		{name: "https credentials", remoteURL: "https://user:super-secret-token@github.com/owner/repo.git", secret: "super-secret-token"},
		{name: "ssh password", remoteURL: "ssh://git:super-secret-token@github.com/owner/repo.git", secret: "super-secret-token"},
		{name: "http", remoteURL: "http://github.com/owner/repo.git"},
		{name: "ext", remoteURL: "ext::sh -c pwn"},
		{name: "ssh host option", remoteURL: "git@-oProxyCommand=touch-pwn:owner/repo.git"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gitCommand(t, repository, "remote", "set-url", "--push", "origin", test.remoteURL)
			_, err := service.Preflight(t.Context(), PreflightRequest{Workspace: fix.WorkspaceIdentity{RepositoryRoot: repository}, Mode: fix.DeliveryModeBranch,
				Remote: "origin", Branch: "slopwatch/fix/test"})
			if err == nil || test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Fatalf("unsafe URL error=%v", err)
			}
		})
	}
}

func TestValidateRemoteURLAllowsCredentialFreeSupportedForms(t *testing.T) {
	for _, value := range []string{
		"https://github.com/owner/repo.git",
		"ssh://git@github.com/owner/repo.git",
		"git@github.com:owner/repo.git",
		"file:///tmp/repository.git",
		"/tmp/repository.git",
	} {
		if err := validateRemoteURL(value); err != nil {
			t.Errorf("validateRemoteURL(%q) = %v", value, err)
		}
	}
}

func TestDeliveryRejectsHostileGitHubPortAndChangedLocalRemote(t *testing.T) {
	if err := validateRemoteURL("https://github.com:8443/owner/repo.git"); err == nil {
		t.Fatal("explicit hostile GitHub port was accepted")
	}
	repository, firstRemote := deliveryRepository(t)
	_, secondRemote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitService(testExecutor{})
	fingerprint, err := service.diffFingerprint(withCommandOutput(t.Context(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteIdentity(firstRemote)
	if err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "remote", "set-url", "--push", "origin", secondRemote)
	_, err = service.CreateCommit(t.Context(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository,
		GitCommonDir: common, BaseCommit: fix.ObjectID(base)}, DiffHash: fingerprint, Branch: "slopwatch/fix/remote-changed", Remote: "origin",
		ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes})
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("changed local remote accepted: %v", err)
	}
	if command := exec.Command("git", "-C", repository, "rev-parse", "--verify", "refs/heads/slopwatch/fix/remote-changed"); command.Run() == nil {
		t.Fatal("changed remote created a local branch")
	}
}

func TestCanonicalLocalRemoteRemovesSymlinkComponents(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	if err := os.Mkdir(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.git")
	if err := os.Symlink(remote, alias); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{alias, (&url.URL{Scheme: "file", Path: alias}).String()} {
		canonical, err := canonicalLocalRemote(value)
		if err != nil || strings.Contains(canonical, "alias.git") || !strings.Contains(canonical, "remote.git") {
			t.Fatalf("canonicalLocalRemote(%q) = %q, %v", value, canonical, err)
		}
	}
}

func TestDeliveryGitNeutralizesRepositoryCredentialCommands(t *testing.T) {
	var launched isolation.Request
	service := &GitService{git: "/usr/bin/git", runner: executorFunc(func(_ context.Context, request isolation.Request) (isolation.Result, error) {
		launched = request
		return isolation.Result{}, nil
	})}
	if _, err := service.gitBytes(withCommandOutput(t.Context(), testDeliveryOutputBytes), "/candidate", false, "status", "--porcelain=v1"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-c", "credential.helper="}, {"-c", "credential.interactive=never"}, {"-c", "core.askPass="}} {
		if !containsArgumentPair(launched.Arguments, pair[0], pair[1]) {
			t.Fatalf("delivery Git omitted credential command guard %q: %#v", pair[1], launched.Arguments)
		}
	}
	joined := strings.Join(launched.Environment, "\x00")
	for _, unsafe := range []string{"GIT_ASKPASS=", "SSH_ASKPASS=", "SSH_AUTH_SOCK="} {
		if strings.Contains(joined, unsafe) {
			t.Fatalf("delivery Git inherited credential command environment %q: %#v", unsafe, launched.Environment)
		}
	}
}

func containsArgumentPair(arguments []string, key, value string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == key && arguments[index+1] == value {
			return true
		}
	}
	return false
}

func TestGitServiceRejectsAttributesAddedAfterPreflight(t *testing.T) {
	repository, remote := deliveryRepository(t)
	common := strings.TrimSpace(gitResult(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	base := strings.TrimSpace(gitResult(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".gitattributes"), []byte("main.go filter=late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := NewGitService(testExecutor{})
	fingerprint, err := service.diffFingerprint(withCommandOutput(context.Background(), testDeliveryOutputBytes), repository)
	if err != nil {
		t.Fatal(err)
	}
	identity, identityErr := remoteIdentity(remote)
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	_, err = service.CreateCommit(context.Background(), Request{Job: "job-test", Candidate: fix.CandidateIdentity{Job: "job-test", RepositoryRoot: repository, GitCommonDir: common, BaseCommit: fix.ObjectID(base)}, DiffHash: fingerprint, Branch: "slopwatch/fix/attrs", Remote: "origin", ExpectedRemoteIdentity: identity, CommandOutputBytes: testDeliveryOutputBytes})
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

type executorFunc func(context.Context, isolation.Request) (isolation.Result, error)

func (function executorFunc) Run(ctx context.Context, request isolation.Request) (isolation.Result, error) {
	return function(ctx, request)
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
