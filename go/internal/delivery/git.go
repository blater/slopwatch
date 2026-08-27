package delivery

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

type commandOutputContextKey struct{}

func withCommandOutput(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

var (
	remoteAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sshUserPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	remoteHostPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
)

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

func (service *GitService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if !request.Plan.Valid() {
		return PreflightResult{}, errors.New("delivery plan is invalid")
	}
	if request.Plan.Git == fix.GitLeaveUncommitted {
		return PreflightResult{}, nil
	}
	if request.Workspace.RepositoryRoot == "" {
		return PreflightResult{}, errors.New("delivery repository is required")
	}
	if request.Plan.Publish != fix.PublishLocal && !validRemoteAlias(request.Remote) {
		return PreflightResult{}, errors.New("delivery remote must be a safe configured remote alias")
	}
	if request.Plan.Git == fix.GitCommitNewBranch {
		if request.Branch == "" {
			return PreflightResult{}, errors.New("new-branch delivery requires a branch name")
		}
		if err := service.validateLiteralBranch(ctx, request.Workspace.RepositoryRoot, request.Branch); err != nil {
			return PreflightResult{}, fmt.Errorf("invalid delivery branch: %w", err)
		}
	}
	if request.Plan.Publish == fix.PublishLocal {
		if request.Plan.Git == fix.GitCommitNewBranch {
			ref := "refs/heads/" + request.Branch
			if exists, _, err := service.ref(ctx, request.Workspace.RepositoryRoot, ref); err != nil {
				return PreflightResult{}, fmt.Errorf("check local delivery branch: %w", err)
			} else if exists {
				return PreflightResult{}, errors.New("delivery branch already exists locally")
			}
		}
		return PreflightResult{}, nil
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Workspace.RepositoryRoot, request.Remote)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q is unavailable: %w", request.Remote, err)
	}
	target, err := remoteProviderTarget(remoteURL)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q has no supported provider identity: %w", request.Remote, err)
	}
	target.RemoteIdentity, err = remoteIdentity(remoteURL)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q identity is invalid: %w", request.Remote, err)
	}
	if request.Plan.Publish == fix.PublishPullRequest && (!strings.EqualFold(target.RemoteHost, "github.com") || target.HostRepository == "") {
		return PreflightResult{}, errors.New("pull-request delivery requires a canonical github.com owner/repository remote")
	}
	if request.Plan.Git == fix.GitCommitNewBranch {
		ref := "refs/heads/" + request.Branch
		if exists, _, err := service.ref(ctx, request.Workspace.RepositoryRoot, ref); err != nil {
			return PreflightResult{}, fmt.Errorf("check local delivery branch: %w", err)
		} else if exists {
			return PreflightResult{}, errors.New("delivery branch already exists locally")
		}
		if exists, _, err := service.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, ref); err != nil {
			return PreflightResult{}, fmt.Errorf("check remote delivery branch: %w", err)
		} else if exists {
			return PreflightResult{}, errors.New("delivery branch already exists remotely")
		}
	}
	if request.Plan.Publish == fix.PublishPullRequest && request.Publication {
		if request.BaseBranch == "" {
			return PreflightResult{}, errors.New("pull-request delivery requires an explicit base branch")
		}
		if err := service.validateLiteralBranch(ctx, request.Workspace.RepositoryRoot, request.BaseBranch); err != nil {
			return PreflightResult{}, fmt.Errorf("pull-request base branch %q is not a literal branch: %w", request.BaseBranch, err)
		}
		baseRef := "refs/heads/" + request.BaseBranch
		if exists, _, err := service.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, baseRef); err != nil {
			return PreflightResult{}, fmt.Errorf("check pull-request base branch %q on admitted remote: %w", request.BaseBranch, err)
		} else if !exists {
			return PreflightResult{}, fmt.Errorf("pull-request base branch %q does not exist on the admitted remote", request.BaseBranch)
		}
	}
	return target, nil
}

func (service *GitService) validateLiteralBranch(ctx context.Context, root, branch string) error {
	data, err := service.gitBytes(ctx, root, false, "check-ref-format", "--branch", branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != branch {
		return errors.New("Git resolved the value as branch shorthand")
	}
	return nil
}

func (service *GitService) PublishCommit(ctx context.Context, request Request) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
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
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	result := Result{}
	if request.Job == "" || request.Candidate.RepositoryRoot == "" || request.Branch == "" || request.DiffHash == "" || len(request.Paths) == 0 || !request.Plan.Valid() || request.Plan.Git == fix.GitLeaveUncommitted {
		return result, errors.New("delivery request is incomplete")
	}
	if request.Plan.Publish != fix.PublishLocal {
		if !validRemoteAlias(request.Remote) {
			return result, errors.New("delivery remote must be a safe configured remote alias")
		}
		if err := service.verifyRequestRemote(ctx, request); err != nil {
			return result, err
		}
	}
	if err := service.validateLiteralBranch(ctx, request.Candidate.RepositoryRoot, request.Branch); err != nil {
		return result, fmt.Errorf("invalid delivery branch: %w", err)
	}
	if request.Plan.Git == fix.GitCommitCurrent {
		return service.createCurrentBranchCommit(ctx, request)
	}
	ref := "refs/heads/" + request.Branch
	if exists, _, err := service.ref(ctx, request.Candidate.RepositoryRoot, ref); err != nil {
		return result, err
	} else if exists {
		return result, errors.New("delivery branch already exists locally")
	}
	if request.Plan.Publish != fix.PublishLocal {
		if exists, _, err := service.remoteRef(ctx, request.Candidate.RepositoryRoot, request.Remote, ref); err != nil {
			return result, err
		} else if exists {
			return result, errors.New("delivery branch already exists remotely")
		}
	}
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	index, err := privateIndexPath(request.Candidate.RepositoryRoot)
	if err != nil {
		return result, err
	}
	defer os.Remove(index)
	environment := []string{"GIT_INDEX_FILE=" + index}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, "read-tree", string(request.Candidate.BaseCommit)); err != nil {
		return result, fmt.Errorf("initialize private publication index: %w", err)
	}
	arguments := []string{"add", "-A", "--"}
	for _, path := range request.Paths {
		arguments = append(arguments, path.String())
	}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, arguments...); err != nil {
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
	commitArguments := []string{"commit-tree", tree, "-p", string(request.Candidate.BaseCommit), "-m", title}
	if body := strings.TrimSpace(request.CommitBody); body != "" {
		commitArguments = append(commitArguments, "-m", body)
	}
	commit, err := service.gitTextEnv(ctx, request.Candidate.RepositoryRoot, environment, commitArguments...)
	if err != nil {
		return result, fmt.Errorf("create candidate commit object: %w", err)
	}
	result.Commit = fix.ObjectID(commit)
	return result, nil
}

func (service *GitService) createCurrentBranchCommit(ctx context.Context, request Request) (Result, error) {
	result := Result{}
	if request.Candidate.WorkspaceMode != fix.WorkspaceCurrent {
		return result, errors.New("committing the current branch requires working in the current files")
	}
	branch, err := service.gitText(ctx, request.Candidate.RepositoryRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != request.Branch {
		return result, errors.New("the current Git branch changed before commit")
	}
	head, err := service.gitText(ctx, request.Candidate.RepositoryRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != request.Candidate.BaseCommit {
		return result, errors.New("the current Git commit changed before commit")
	}
	lock, err := acquireDeliveryLock(request.Candidate.GitCommonDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	index, err := privateIndexPath(request.Candidate.RepositoryRoot)
	if err != nil {
		return result, err
	}
	defer os.Remove(index)
	environment := []string{"GIT_INDEX_FILE=" + index}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, "read-tree", head); err != nil {
		return result, fmt.Errorf("initialize current-branch commit: %w", err)
	}
	arguments := []string{"add", "-A", "--"}
	for _, path := range request.Paths {
		arguments = append(arguments, path.String())
	}
	if _, err := service.gitBytesEnv(ctx, request.Candidate.RepositoryRoot, environment, false, arguments...); err != nil {
		return result, fmt.Errorf("stage current-branch files: %w", err)
	}
	tree, err := service.gitTextEnv(ctx, request.Candidate.RepositoryRoot, environment, "write-tree")
	if err != nil {
		return result, fmt.Errorf("write current-branch tree: %w", err)
	}
	title := strings.TrimSpace(request.CommitTitle)
	if title == "" {
		title = "Refactor with Slopwatch"
	}
	commitArguments := []string{"commit-tree", tree, "-p", head, "-m", title}
	if body := strings.TrimSpace(request.CommitBody); body != "" {
		commitArguments = append(commitArguments, "-m", body)
	}
	commit, err := service.gitTextEnv(ctx, request.Candidate.RepositoryRoot, environment, commitArguments...)
	if err != nil {
		return result, fmt.Errorf("create current-branch commit: %w", err)
	}
	ref := "refs/heads/" + branch
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, "update-ref", ref, commit, head); err != nil {
		result.Commit, result.Ambiguous, result.Diagnostic = fix.ObjectID(commit), true, err.Error()
		return result, fmt.Errorf("advance current branch: %w", err)
	}
	resetArguments := []string{"reset", "-q", commit, "--"}
	for _, path := range request.Paths {
		resetArguments = append(resetArguments, path.String())
	}
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, resetArguments...); err != nil {
		result.Commit, result.LocalRef, result.Ambiguous, result.Diagnostic = fix.ObjectID(commit), ref, true, err.Error()
		return result, fmt.Errorf("refresh committed files in the Git index: %w", err)
	}
	result.Commit, result.LocalRef = fix.ObjectID(commit), ref
	return result, nil
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
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if result.Commit == "" || result.LocalRef != "" {
		return result, errors.New("local ref step requires only a committed object")
	}
	if request.Plan.Git == fix.GitCommitCurrent {
		ref := "refs/heads/" + request.Branch
		if result.LocalRef == ref {
			return result, nil
		}
		return result, errors.New("current branch commit did not record its local ref")
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
		result.Ambiguous = true
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("create local delivery ref: %w", err)
	}
	result.LocalRef = ref
	return result, nil
}

func (service *GitService) verifyRequestRemote(ctx context.Context, request Request) error {
	if request.ExpectedRemoteIdentity == "" {
		return errors.New("delivery request lacks the exact admitted remote identity")
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return err
	}
	return verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository)
}

func (service *GitService) CreateRemoteRef(ctx context.Context, request Request, result Result) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
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
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return result, err
	}
	if err := verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository); err != nil {
		return result, err
	}
	exists, oid, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
	if err != nil {
		return result, err
	}
	if exists {
		if oid == commit {
			result.RemoteRef, result.Pushed = ref, true
			result.Repository = remoteRepositoryURL(remoteURL)
			return result, nil
		}
		if request.Plan.Git == fix.GitCommitNewBranch {
			return result, errors.New("delivery branch appeared remotely at a different commit")
		}
	}
	arguments := []string{"push", "--porcelain"}
	if request.Plan.Git == fix.GitCommitNewBranch {
		arguments = append(arguments, "--force-with-lease="+ref+":")
	}
	arguments = append(arguments, remoteURL, commit+":"+ref)
	if _, err := service.gitBytes(ctx, request.Candidate.RepositoryRoot, false, arguments...); err != nil {
		result.Ambiguous = true
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("create remote delivery ref: %w", err)
	}
	exists, remoteCommit, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
	if err != nil || !exists || remoteCommit != commit {
		result.Ambiguous = true
		result.Diagnostic = "remote ref could not be verified at the committed object"
		return result, errors.New(result.Diagnostic)
	}
	result.RemoteRef = ref
	result.Pushed = true
	result.Repository = remoteRepositoryURL(remoteURL)
	return result, nil
}

func (service *GitService) Reconcile(ctx context.Context, request Request, previous Result) (Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if previous.Commit == "" {
		return previous, errors.New("delivery reconciliation requires the saved commit")
	}
	ref := previous.RemoteRef
	if ref == "" {
		ref = "refs/heads/" + request.Branch
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Candidate.RepositoryRoot, request.Remote)
	if err != nil {
		return previous, err
	}
	if err := verifyExpectedRemote(remoteURL, request.ExpectedRemoteIdentity, request.ExpectedRemoteHost, request.HostRepository); err != nil {
		return previous, err
	}
	if previous.LocalRef == "" {
		localRef := "refs/heads/" + request.Branch
		exists, commit, err := service.ref(ctx, request.Candidate.RepositoryRoot, localRef)
		if err != nil {
			return previous, err
		}
		if !exists {
			previous.Ambiguous = false
			previous.Diagnostic = "local ref is absent"
			return previous, nil
		}
		if commit != string(previous.Commit) {
			previous.Ambiguous = true
			previous.Diagnostic = "local ref exists at a different commit"
			return previous, errors.New(previous.Diagnostic)
		}
		previous.LocalRef = localRef
		previous.Ambiguous = false
		previous.Diagnostic = ""
		return previous, nil
	}
	exists, commit, err := service.remoteRefURL(ctx, request.Candidate.RepositoryRoot, remoteURL, ref)
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
	previous.Repository = remoteRepositoryURL(remoteURL)
	previous.Ambiguous = false
	previous.Diagnostic = ""
	return previous, nil
}

func remoteRepositoryURL(value string) string {
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
	remoteURL, err := service.resolveRemoteURL(ctx, root, remote)
	if err != nil {
		return false, "", err
	}
	return service.remoteRefURL(ctx, root, remoteURL, ref)
}

func (service *GitService) remoteRefURL(ctx context.Context, root, remoteURL, ref string) (bool, string, error) {
	data, err := service.gitBytes(ctx, root, true, "ls-remote", "--exit-code", "--refs", remoteURL, ref)
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

func validRemoteAlias(value string) bool {
	return remoteAliasPattern.MatchString(value)
}

func (service *GitService) resolveRemoteURL(ctx context.Context, root, remote string) (string, error) {
	if !validRemoteAlias(remote) {
		return "", errors.New("delivery remote must be a safe configured remote alias")
	}
	value, err := service.gitText(ctx, root, "remote", "get-url", "--push", remote)
	if err != nil {
		return "", err
	}
	if err := validateRemoteURL(value); err != nil {
		return "", err
	}
	return canonicalLocalRemote(value)
}

// canonicalLocalRemote removes mutable symlink components from local remote
// paths before the admitted identity is calculated and before Git receives
// the URL. Network remotes are already pinned by their normalized literal.
func canonicalLocalRemote(value string) (string, error) {
	if filepath.IsAbs(value) {
		canonical, err := filepath.EvalSymlinks(value)
		if err != nil {
			return "", errors.New("delivery local remote cannot be canonicalized")
		}
		return canonical, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" {
		return value, nil
	}
	canonical, err := filepath.EvalSymlinks(parsed.Path)
	if err != nil {
		return "", errors.New("delivery local remote cannot be canonicalized")
	}
	return (&url.URL{Scheme: "file", Path: canonical}).String(), nil
}

func validateRemoteURL(value string) error {
	if value == "" || strings.ContainsAny(value, "\x00\r\n\t ") || strings.HasPrefix(value, "-") {
		return errors.New("delivery remote URL is unsafe")
	}
	if filepath.IsAbs(value) {
		if filepath.Clean(value) != value {
			return errors.New("delivery local remote path is not canonical")
		}
		return nil
	}
	if !strings.Contains(value, "://") {
		authority, remotePath, ok := strings.Cut(value, ":")
		user, host, hasUser := strings.Cut(authority, "@")
		if !ok || !hasUser || !sshUserPattern.MatchString(user) || !remoteHostPattern.MatchString(host) || remotePath == "" ||
			strings.ContainsAny(host+remotePath, "@?#") || strings.Contains(remotePath, "..") {
			return errors.New("delivery remote URL uses an unsupported SSH form")
		}
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("delivery remote URL is invalid")
	}
	if parsed.Scheme == "file" {
		if parsed.User != nil || parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") || !filepath.IsAbs(parsed.Path) {
			return errors.New("delivery file remote URL is unsafe")
		}
		return nil
	}
	if parsed.Host == "" || !remoteHostPattern.MatchString(parsed.Hostname()) {
		return errors.New("delivery remote URL is invalid")
	}
	if strings.EqualFold(parsed.Hostname(), "github.com") && parsed.Port() != "" {
		return errors.New("delivery GitHub remote URL must not contain an explicit port")
	}
	switch parsed.Scheme {
	case "https":
		if parsed.User != nil {
			return errors.New("delivery HTTPS remote URL must not contain credentials")
		}
	case "ssh":
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword || !sshUserPattern.MatchString(parsed.User.Username()) {
				return errors.New("delivery SSH remote URL contains unsupported credentials")
			}
		}
	default:
		return errors.New("delivery remote URL uses an unsupported protocol")
	}
	return nil
}

func remoteProviderTarget(value string) (PreflightResult, error) {
	if filepath.IsAbs(value) || strings.HasPrefix(value, "file://") {
		return PreflightResult{}, nil
	}
	var host, remotePath string
	if !strings.Contains(value, "://") {
		authority, path, ok := strings.Cut(value, ":")
		if !ok {
			return PreflightResult{}, errors.New("remote provider identity is unavailable")
		}
		_, host, _ = strings.Cut(authority, "@")
		remotePath = path
	} else {
		parsed, err := url.Parse(value)
		if err != nil {
			return PreflightResult{}, errors.New("remote provider identity is invalid")
		}
		host = parsed.Hostname()
		remotePath = parsed.Path
	}
	remotePath = strings.Trim(strings.TrimSuffix(remotePath, ".git"), "/")
	parts := strings.Split(remotePath, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0]+parts[1], "%") {
		return PreflightResult{}, errors.New("remote must identify an owner/repository")
	}
	return PreflightResult{RemoteHost: strings.ToLower(host), HostRepository: parts[0] + "/" + parts[1]}, nil
}

func verifyExpectedRemote(remoteURL, expectedIdentity, expectedHost, expectedRepository string) error {
	if expectedIdentity == "" {
		return errors.New("delivery request lacks the exact admitted remote identity")
	}
	identity, err := remoteIdentity(remoteURL)
	if err != nil || identity != expectedIdentity {
		return errors.New("delivery remote identity changed since preflight")
	}
	if expectedRepository == "" && expectedHost == "" {
		return nil
	}
	target, err := remoteProviderTarget(remoteURL)
	if err != nil || !strings.EqualFold(target.RemoteHost, expectedHost) || !strings.EqualFold(target.HostRepository, expectedRepository) {
		return errors.New("delivery remote identity changed since preflight")
	}
	return nil
}

func remoteIdentity(value string) (string, error) {
	if err := validateRemoteURL(value); err != nil {
		return "", err
	}
	var normalized string
	if filepath.IsAbs(value) {
		canonical, err := filepath.EvalSymlinks(value)
		if err != nil {
			return "", errors.New("delivery local remote cannot be canonicalized")
		}
		normalized = "file://" + filepath.ToSlash(canonical)
	} else if !strings.Contains(value, "://") {
		authority, remotePath, _ := strings.Cut(value, ":")
		user, host, _ := strings.Cut(authority, "@")
		normalized = "ssh-scp://" + user + "@" + strings.ToLower(host) + "/" + remotePath
	} else {
		parsed, _ := url.Parse(value)
		if parsed.Scheme == "file" {
			canonical, err := filepath.EvalSymlinks(parsed.Path)
			if err != nil {
				return "", errors.New("delivery local remote cannot be canonicalized")
			}
			normalized = "file://" + filepath.ToSlash(canonical)
		} else {
			userinfo := ""
			if parsed.User != nil {
				userinfo = parsed.User.Username() + "@"
			}
			normalized = strings.ToLower(parsed.Scheme) + "://" + userinfo + strings.ToLower(parsed.Host) + parsed.EscapedPath()
		}
	}
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("sha256:%x", digest[:]), nil
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
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	if maximum <= 0 {
		return nil, errors.New("Git command output budget is not configured")
	}
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true",
		// An admitted repository may name a remote, but it may not add a
		// process-launch path to publication through local credential config.
		// An empty helper value resets all helpers read earlier by Git.
		"-c", "credential.helper=", "-c", "credential.interactive=never", "-c", "core.askPass=",
		"-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always", "-c", "protocol.ext.allow=never", "-c", "core.sshCommand=ssh", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	environment := []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Slopwatch", "GIT_AUTHOR_EMAIL=slopwatch@localhost", "GIT_COMMITTER_NAME=Slopwatch", "GIT_COMMITTER_EMAIL=slopwatch@localhost"}
	environment = append(environment, extraEnvironment...)
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.git, Arguments: trusted, Directory: root,
		Environment: environment,
		Stdin:       input,
		Limits:      isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
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
