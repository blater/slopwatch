package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/gitmanifest"
	"github.com/blater/slopwatch/internal/isolation"
)

// GitService implements an exact-ref, create-only publication workflow. It
// never force-updates an existing branch and never runs repository hooks or
// signing programs.
type GitService struct {
	git    string
	runner isolation.Executor
}

func NewGitService(runner isolation.Executor) (*GitService, error) {
	if runner == nil {
		return nil, errors.New("Git delivery requires a process runner")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	git, err = filepath.Abs(git)
	if err != nil {
		return nil, err
	}
	return &GitService{git: git, runner: runner}, nil
}

func (service *GitService) Preflight(ctx context.Context, request PreflightRequest) error {
	if !request.Mode.Valid() {
		return errors.New("delivery mode is invalid")
	}
	if request.Mode == fix.DeliveryModeCandidate {
		return nil
	}
	if request.Workspace.RepositoryRoot == "" || request.Remote == "" || request.Branch == "" {
		return errors.New("delivery repository, remote, and branch are required")
	}
	if _, err := service.gitBytes(ctx, request.Workspace.RepositoryRoot, false, "check-ref-format", "--branch", request.Branch); err != nil {
		return fmt.Errorf("invalid delivery branch: %w", err)
	}
	if _, err := service.gitBytes(ctx, request.Workspace.RepositoryRoot, false, "remote", "get-url", request.Remote); err != nil {
		return fmt.Errorf("delivery remote %q is unavailable: %w", request.Remote, err)
	}
	if request.Mode == fix.DeliveryModePullRequest {
		if request.BaseBranch == "" {
			return errors.New("pull-request delivery requires an explicit base branch")
		}
		if _, err := service.gitBytes(ctx, request.Workspace.RepositoryRoot, false, "rev-parse", "--verify", request.BaseBranch+"^{commit}"); err != nil {
			return fmt.Errorf("pull-request base branch %q is not resolvable: %w", request.BaseBranch, err)
		}
	}
	return nil
}

func (service *GitService) PublishCommit(ctx context.Context, request Request) (Result, error) {
	result, err := service.CreateCommit(ctx, request)
	if err != nil {
		return result, err
	}
	result, err = service.CreateLocalRef(ctx, request, result)
	if err != nil {
		return result, err
	}
	return service.CreateRemoteRef(ctx, request, result)
}

func (service *GitService) CreateCommit(ctx context.Context, request Request) (Result, error) {
	result := Result{}
	if request.Job == "" || request.Candidate.RepositoryRoot == "" || request.Branch == "" || request.Remote == "" || request.DiffHash == "" {
		return result, errors.New("delivery request is incomplete")
	}
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, "check-ref-format", "--branch", request.Branch); err != nil {
		return result, fmt.Errorf("invalid delivery branch: %w", err)
	}
	ref := "refs/heads/" + request.Branch
	if exists, _, err := service.ref(ctx, request.Candidate.RepositoryRoot, ref); err != nil {
		return result, err
	} else if exists {
		return result, errors.New("delivery branch already exists locally")
	}
	if exists, _, err := service.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref); err != nil {
		return result, err
	} else if exists {
		return result, errors.New("delivery branch already exists remotely")
	}
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	fingerprint, err := service.diffFingerprint(ctx, request.Candidate.RepositoryRoot)
	if err != nil {
		return result, err
	}
	if fingerprint != request.DiffHash {
		return result, errors.New("candidate diff changed since review")
	}
	if err := service.rejectTransformingAttributes(ctx, request.Candidate.RepositoryRoot); err != nil {
		return result, err
	}
	index, err := privateIndexPath(request.Candidate.RepositoryRoot)
	if err != nil {
		return result, err
	}
	defer os.Remove(index)
	environment := []string{"GIT_INDEX_FILE=" + index}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, "read-tree", string(request.Candidate.BaseCommit)); err != nil {
		return result, fmt.Errorf("initialize private publication index: %w", err)
	}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, "add", "-A", "--"); err != nil {
		return result, fmt.Errorf("stage candidate in private publication index: %w", err)
	}
	tree, err := service.gitTextEnv(ctx, request.Candidate.RepositoryRoot, environment, "write-tree")
	if err != nil {
		return result, fmt.Errorf("write candidate tree: %w", err)
	}
	baseTree, err := service.gitText(ctx, request.Candidate.RepositoryRoot, "rev-parse", string(request.Candidate.BaseCommit)+"^{tree}")
	if err != nil {
		return result, err
	}
	if tree == baseTree {
		return result, errors.New("candidate has no changes to publish")
	}
	title := strings.TrimSpace(request.CommitTitle)
	if title == "" {
		title = "Refactor with Slopwatch"
	}
	arguments := []string{"commit-tree", tree, "-p", string(request.Candidate.BaseCommit), "-m", title}
	if body := strings.TrimSpace(request.CommitBody); body != "" {
		arguments = append(arguments, "-m", body)
	}
	commit, err := service.gitTextEnv(ctx, request.Candidate.RepositoryRoot, environment, arguments...)
	if err != nil {
		return result, fmt.Errorf("create candidate commit object: %w", err)
	}
	result.Commit = fix.ObjectID(commit)
	return result, nil
}

func (service *GitService) rejectTransformingAttributes(ctx context.Context, root string) error {
	paths, err := service.gitBytes(ctx, root, false, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	attributes, err := service.gitBytesInputEnv(ctx, root, nil, paths, false, "check-attr", "-z", "--stdin", "--all")
	if err != nil {
		return err
	}
	fields := bytes.Split(attributes, []byte{0})
	if len(fields) > 1 && (len(fields)-1)%3 != 0 {
		return errors.New("malformed Git attribute response")
	}
	for index := 0; index+2 < len(fields)-1; index += 3 {
		attribute, value := strings.ToLower(string(fields[index+1])), string(fields[index+2])
		switch attribute {
		case "filter", "working-tree-encoding", "text", "eol", "crlf", "ident":
			if value != "unspecified" {
				return fmt.Errorf("candidate path %q selects unsupported content-transforming Git attribute %q", fields[index], attribute)
			}
		}
	}
	return nil
}

func privateIndexPath(root string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(root), ".slopwatch-publish-index-")
	if err != nil {
		return "", fmt.Errorf("reserve private publication index: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func (service *GitService) CreateLocalRef(ctx context.Context, request Request, result Result) (Result, error) {
	if result.Commit == "" || result.LocalRef != "" {
		return result, errors.New("local ref step requires only a committed object")
	}
	ref := "refs/heads/" + request.Branch
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if exists, oid, err := service.ref(ctx, request.Candidate.RepositoryRoot, ref); err != nil {
		return result, err
	} else if exists {
		if oid == string(result.Commit) {
			result.LocalRef = ref
			return result, nil
		}
		return result, errors.New("delivery branch appeared locally at a different commit")
	}
	format, err := service.gitText(ctx, request.Candidate.RepositoryRoot, "rev-parse", "--show-object-format")
	if err != nil {
		return result, err
	}
	zeros := strings.Repeat("0", 40)
	if format == "sha256" {
		zeros = strings.Repeat("0", 64)
	}
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, "update-ref", ref, string(result.Commit), zeros); err != nil {
		return result, fmt.Errorf("create local delivery ref: %w", err)
	}
	result.LocalRef = ref
	return result, nil
}

func (service *GitService) CreateRemoteRef(ctx context.Context, request Request, result Result) (Result, error) {
	if result.Commit == "" || result.LocalRef == "" || result.Pushed {
		return result, errors.New("remote ref step requires a committed local ref")
	}
	ref := result.LocalRef
	commit := string(result.Commit)
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if exists, oid, err := service.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref); err != nil {
		return result, err
	} else if exists {
		if oid == commit {
			result.RemoteRef, result.Pushed = ref, true
			result.Repository = service.remoteRepository(ctx, request.Candidate.RepositoryRoot, request.Remote)
			return result, nil
		}
		return result, errors.New("delivery branch appeared remotely at a different commit")
	}
	lease := "--force-with-lease=" + ref + ":"
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, "push", "--porcelain", lease, request.Remote, commit+":"+ref); err != nil {
		result.Ambiguous = true
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("create remote delivery ref: %w", err)
	}
	exists, remoteCommit, err := service.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref)
	if err != nil || !exists || remoteCommit != commit {
		result.Ambiguous = true
		result.Diagnostic = "remote ref could not be verified at the committed object"
		return result, errors.New(result.Diagnostic)
	}
	result.RemoteRef = ref
	result.Pushed = true
	result.Repository = service.remoteRepository(ctx, request.Candidate.RepositoryRoot, request.Remote)
	return result, nil
}

func (service *GitService) Reconcile(ctx context.Context, request Request, previous Result) (Result, error) {
	if previous.Commit == "" {
		return previous, errors.New("delivery reconciliation requires the journaled commit")
	}
	ref := previous.RemoteRef
	if ref == "" {
		ref = "refs/heads/" + request.Branch
	}
	exists, commit, err := service.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref)
	if err != nil {
		return previous, err
	}
	if !exists {
		previous.Ambiguous = false
		previous.Pushed = false
		previous.Diagnostic = "remote ref is absent"
		return previous, nil
	}
	if commit != string(previous.Commit) {
		previous.Ambiguous = true
		previous.Diagnostic = "remote ref exists at a different commit"
		return previous, errors.New(previous.Diagnostic)
	}
	previous.RemoteRef = ref
	previous.Pushed = true
	previous.Repository = service.remoteRepository(ctx, request.Candidate.RepositoryRoot, request.Remote)
	previous.Ambiguous = false
	previous.Diagnostic = ""
	return previous, nil
}

func (service *GitService) remoteRepository(ctx context.Context, root, remote string) string {
	value, err := service.gitText(ctx, root, "remote", "get-url", "--push", remote)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(value, "git@github.com:") {
		return normalizeGitHubRepository(strings.TrimPrefix(value, "git@github.com:"))
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return ""
	}
	return normalizeGitHubRepository(strings.TrimPrefix(parsed.Path, "/"))
}

func normalizeGitHubRepository(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

var errGitExitOne = errors.New("git exited with status one")

func (service *GitService) ref(ctx context.Context, root, ref string) (bool, string, error) {
	data, err := service.gitBytes(ctx, root, true, "rev-parse", "--verify", "--quiet", ref)
	if errors.Is(err, errGitExitOne) {
		return false, "", nil
	}
	return err == nil, strings.TrimSpace(string(data)), err
}

func (service *GitService) remoteRef(ctx context.Context, root, remote, ref string) (bool, string, error) {
	data, err := service.gitBytes(ctx, root, true, "ls-remote", "--exit-code", "--refs", remote, ref)
	if errors.Is(err, errGitExitOne) || (err != nil && strings.Contains(err.Error(), "status 2")) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return false, "", nil
	}
	if len(fields) != 2 || fields[1] != ref {
		return false, "", errors.New("malformed remote ref response")
	}
	return true, fields[0], nil
}

func (service *GitService) diffFingerprint(ctx context.Context, root string) (string, error) {
	data, err := service.gitBytes(ctx, root, false, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	manifest, err := gitmanifest.Build(root, data)
	if err != nil {
		return "", err
	}
	return manifest.Fingerprint, nil
}

func (service *GitService) gitText(ctx context.Context, root string, arguments ...string) (string, error) {
	data, err := service.gitBytes(ctx, root, false, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (service *GitService) gitTextEnv(ctx context.Context, root string, environment []string, arguments ...string) (string, error) {
	data, err := service.gitBytesEnv(ctx, root, environment, false, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (service *GitService) gitBytes(ctx context.Context, root string, exitOne bool, arguments ...string) ([]byte, error) {
	return service.gitBytesEnv(ctx, root, nil, exitOne, arguments...)
}

func (service *GitService) gitBytesEnv(ctx context.Context, root string, extraEnvironment []string, exitOne bool, arguments ...string) ([]byte, error) {
	return service.gitBytesInputEnv(ctx, root, extraEnvironment, nil, exitOne, arguments...)
}

func (service *GitService) gitBytesInputEnv(ctx context.Context, root string, extraEnvironment []string, input []byte, exitOne bool, arguments ...string) ([]byte, error) {
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always", "-c", "protocol.ext.allow=never", "-c", "core.sshCommand=ssh", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	environment := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Slopwatch", "GIT_AUTHOR_EMAIL=slopwatch@localhost", "GIT_COMMITTER_NAME=Slopwatch", "GIT_COMMITTER_EMAIL=slopwatch@localhost"}
	environment = append(environment, extraEnvironment...)
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.git, Arguments: trusted, Directory: root,
		Environment: environment,
		Stdin:       input,
		Limits:      isolation.Limits{WallTime: 2 * time.Minute, TerminateGrace: 2 * time.Second, MaxStdoutBytes: 4 << 20, MaxStderrBytes: 4 << 20}})
	if err != nil {
		return nil, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("Git output exceeded safety limit")
	}
	if result.ExitCode == 1 && exitOne {
		return result.Stdout, errGitExitOne
	}
	if !result.Successful() {
		return nil, fmt.Errorf("git %s exited with status %d: %s", arguments[0], result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}
