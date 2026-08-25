package candidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/gitmanifest"
	"github.com/blater/slopwatch/internal/isolation"
)

const gitOutputLimit = int64(4 << 20)

// GitWorktreeService owns detached per-job worktrees. It is the only
// remediation component allowed to inspect Git metadata; agent adapters only
// receive the resulting candidate root through agent.Request.
type GitWorktreeService struct {
	stateRoot string
	git       string
	runner    isolation.Executor
	mu        sync.Mutex
	policies  map[fix.JobID]candidatePolicy
	leases    map[string]*repositoryOwnership
	jobLeases map[fix.JobID]string
	closed    bool
}

type repositoryOwnership struct {
	lease *repositoryLock
	jobs  map[fix.JobID]bool
}

type candidatePolicy struct {
	allowed map[fix.RepoPath]bool
	scope   string
}

const (
	ownershipName   = "owner.json"
	reservationName = "reservation.json"
)

type ownershipRecord struct {
	Version  int                   `json:"version"`
	Identity fix.CandidateIdentity `json:"identity"`
	Targets  []fix.RepoPath        `json:"targets"`
	Allowed  []fix.RepoPath        `json:"allowed"`
	Scope    string                `json:"scope"`
}

func NewGitWorktreeService(stateRoot string, runner isolation.Executor) (*GitWorktreeService, error) {
	if stateRoot == "" || !filepath.IsAbs(stateRoot) || runner == nil {
		return nil, errors.New("candidate worktree service requires an absolute private state root and process runner")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("locate git: %w", err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		return nil, fmt.Errorf("resolve git: %w", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create candidate state root: %w", err)
	}
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("protect candidate state root: %w", err)
	}
	stateRoot, err = filepath.EvalSymlinks(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("canonicalize candidate state root: %w", err)
	}
	return &GitWorktreeService{stateRoot: stateRoot, git: git, runner: runner, policies: map[fix.JobID]candidatePolicy{}, leases: map[string]*repositoryOwnership{}, jobLeases: map[fix.JobID]string{}}, nil
}

// DiscoverWorkspace resolves all security-sensitive repository paths via Git
// rather than trusting a cache entry or a caller-composed .git path.
func (service *GitWorktreeService) DiscoverWorkspace(ctx context.Context, root string) (fix.WorkspaceIdentity, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return fix.WorkspaceIdentity{}, err
	}
	top, err := service.gitText(ctx, absolute, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover repository root: %w", err)
	}
	common, err := service.gitText(ctx, top, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover git common directory: %w", err)
	}
	commit, err := service.gitText(ctx, top, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover base commit: %w", err)
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("canonicalize repository root: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("canonicalize git common directory: %w", err)
	}
	hash := sha256.Sum256([]byte(top + "\x00" + common))
	return fix.WorkspaceIdentity{Repository: fix.RepositoryID(hex.EncodeToString(hash[:16])), RepositoryRoot: top, AnalysisRoot: top, GitCommonDir: common, BaseCommit: fix.ObjectID(commit)}, nil
}

func (service *GitWorktreeService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	checked := time.Now()
	discovered, err := service.DiscoverWorkspace(ctx, request.Workspace.RepositoryRoot)
	if err != nil {
		return PreflightResult{CheckedAt: checked, Diagnostic: err.Error()}, err
	}
	if request.Workspace.Repository != "" && request.Workspace.Repository != discovered.Repository ||
		request.Workspace.GitCommonDir != "" && request.Workspace.GitCommonDir != discovered.GitCommonDir {
		return PreflightResult{CheckedAt: checked, Diagnostic: "repository identity changed since analysis"}, nil
	}
	if request.Workspace.BaseCommit != "" && request.Workspace.BaseCommit != discovered.BaseCommit {
		return PreflightResult{CheckedAt: checked, Diagnostic: "repository HEAD changed since analysis"}, nil
	}
	status, err := service.gitBytes(ctx, discovered.RepositoryRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return PreflightResult{CheckedAt: checked, Diagnostic: err.Error()}, err
	}
	if len(status) != 0 {
		return PreflightResult{CheckedAt: checked, Diagnostic: "working tree must be clean before a fix job is admitted"}, nil
	}
	for _, state := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "index.lock"} {
		path, pathErr := service.gitText(ctx, discovered.RepositoryRoot, "rev-parse", "--path-format=absolute", "--git-path", state)
		if pathErr != nil {
			return PreflightResult{CheckedAt: checked, Diagnostic: pathErr.Error()}, pathErr
		}
		if _, statErr := os.Lstat(path); statErr == nil {
			return PreflightResult{Clean: true, CheckedAt: checked, Diagnostic: "repository has an in-progress Git operation: " + state}, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return PreflightResult{CheckedAt: checked, Diagnostic: statErr.Error()}, statErr
		}
	}
	if reason, unsupported, err := service.unsupportedRepository(ctx, discovered); err != nil {
		return PreflightResult{CheckedAt: checked, Diagnostic: err.Error()}, err
	} else if unsupported {
		return PreflightResult{Clean: true, CheckedAt: checked, Diagnostic: reason}, nil
	}
	blobs := make(map[fix.RepoPath]fix.ObjectID, len(request.Targets))
	for _, target := range request.Targets {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return PreflightResult{Clean: true, CheckedAt: checked, Diagnostic: fmt.Sprintf("invalid target %q", target)}, nil
		}
		mode, err := service.gitText(ctx, discovered.RepositoryRoot, "ls-files", "--stage", "--", target.String())
		if err != nil || mode == "" || strings.HasPrefix(mode, "120000 ") || strings.HasPrefix(mode, "160000 ") {
			return PreflightResult{Clean: true, CheckedAt: checked, Diagnostic: fmt.Sprintf("target %s must be a tracked regular file", target)}, nil
		}
		blob, err := service.gitText(ctx, discovered.RepositoryRoot, "rev-parse", "--verify", "HEAD:"+target.String())
		if err != nil {
			return PreflightResult{Clean: true, CheckedAt: checked, Diagnostic: fmt.Sprintf("resolve target %s: %v", target, err)}, nil
		}
		blobs[target] = fix.ObjectID(blob)
	}
	return PreflightResult{Clean: true, Supported: true, CheckedAt: checked, TargetBlobs: blobs}, nil
}

func (service *GitWorktreeService) unsupportedRepository(ctx context.Context, workspace fix.WorkspaceIdentity) (string, bool, error) {
	stages, err := service.gitBytes(ctx, workspace.RepositoryRoot, "ls-files", "--stage")
	if err != nil {
		return "", false, err
	}
	for _, line := range bytes.Split(stages, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("160000 ")) {
			return "repositories containing Git submodules are not supported for fix jobs", true, nil
		}
	}
	filters, err := service.gitBytesAllowExitOne(ctx, workspace.RepositoryRoot, "config", "--local", "--get-regexp", "^filter\\.")
	if err != nil {
		return "", false, err
	}
	if len(bytes.TrimSpace(filters)) > 0 {
		return "repositories with local clean/smudge filters or Git LFS are not supported for fix jobs", true, nil
	}
	paths, err := service.gitBytes(ctx, workspace.RepositoryRoot, "ls-files", "-z")
	if err != nil {
		return "", false, err
	}
	if found, scanErr := contentTransformingAttributeDefinition(workspace, paths); scanErr != nil {
		return "", false, scanErr
	} else if found {
		return "repositories with content-transforming Git attributes are not supported for fix jobs", true, nil
	}
	attributes, err := service.gitBytesInput(ctx, workspace.RepositoryRoot, paths, "check-attr", "-z", "--stdin", "--all")
	if err != nil {
		return "", false, err
	}
	fields := bytes.Split(attributes, []byte{0})
	if len(fields) > 1 && (len(fields)-1)%3 != 0 {
		return "", false, errors.New("malformed Git attribute response")
	}
	for index := 0; index+2 < len(fields)-1; index += 3 {
		attribute, value := string(fields[index+1]), string(fields[index+2])
		if isTransformingAttribute(attribute) && value != "unspecified" {
			return "repositories with content-transforming Git attributes are not supported for fix jobs", true, nil
		}
	}
	return "", false, nil
}

func contentTransformingAttributeDefinition(workspace fix.WorkspaceIdentity, tracked []byte) (bool, error) {
	root, err := os.OpenRoot(workspace.RepositoryRoot)
	if err != nil {
		return false, err
	}
	defer root.Close()
	for _, raw := range bytes.Split(tracked, []byte{0}) {
		path := string(raw)
		if path == "" || path != ".gitattributes" && !strings.HasSuffix(path, "/.gitattributes") {
			continue
		}
		if _, err := fix.ParseRepoPath(path); err != nil {
			return false, err
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return false, err
		}
		if transformingAttributeText(data) {
			return true, nil
		}
	}
	common, err := os.OpenRoot(workspace.GitCommonDir)
	if err != nil {
		return false, err
	}
	defer common.Close()
	data, err := common.ReadFile("info/attributes")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return transformingAttributeText(data), nil
}

func transformingAttributeText(data []byte) bool {
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		for _, raw := range fields[1:] {
			attribute := strings.TrimLeft(strings.ToLower(raw), "-!")
			name, _, _ := strings.Cut(attribute, "=")
			if isTransformingAttribute(name) {
				return true
			}
		}
	}
	return false
}

func isTransformingAttribute(attribute string) bool {
	switch strings.ToLower(attribute) {
	case "filter", "working-tree-encoding", "text", "eol", "crlf", "ident":
		return true
	}
	return false
}

func (service *GitWorktreeService) Prepare(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, error) {
	if !validJobID(request.Job) || len(request.Targets) == 0 {
		return fix.CandidateIdentity{}, errors.New("prepare candidate: job and targets are required")
	}
	if existing, found, err := service.DiscoverPrepared(ctx, request); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("reconcile prepared candidate: %w", err)
	} else if found {
		return existing, nil
	}
	preflight, err := service.Preflight(ctx, PreflightRequest{Workspace: request.Workspace, Targets: request.Targets})
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	if !preflight.Clean || !preflight.Supported {
		return fix.CandidateIdentity{}, fmt.Errorf("prepare candidate: %s", preflight.Diagnostic)
	}
	discovered, err := service.DiscoverWorkspace(ctx, request.Workspace.RepositoryRoot)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	preparedRequest := request
	preparedRequest.Workspace.Repository = discovered.Repository
	preparedRequest.Workspace.RepositoryRoot = discovered.RepositoryRoot
	preparedRequest.Workspace.GitCommonDir = discovered.GitCommonDir
	preparedRequest.Workspace.BaseCommit = discovered.BaseCommit
	ownership, err := service.expectedOwnership(preparedRequest)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	if err := service.retainRepository(discovered.GitCommonDir, request.Job); err != nil {
		return fix.CandidateIdentity{}, err
	}
	retained := true
	defer func() {
		if retained {
			_ = service.releaseRepository(discovered.GitCommonDir, request.Job)
		}
	}()
	jobRoot := filepath.Join(service.stateRoot, string(request.Job))
	worktree := ownership.Identity.RepositoryRoot
	if err := os.MkdirAll(jobRoot, 0o700); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("create job state directory: %w", err)
	}
	if err := ensureCandidateRecord(jobRoot, reservationName, ownership); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("persist candidate reservation: %w", err)
	}
	lock, err := acquireRepositoryLock(discovered.GitCommonDir)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	_, commandErr := service.gitBytes(ctx, discovered.RepositoryRoot, "worktree", "add", "--detach", "--no-checkout", worktree, string(discovered.BaseCommit))
	if commandErr == nil {
		_, commandErr = service.gitBytes(ctx, worktree, "checkout", "--detach", string(discovered.BaseCommit), "--")
	}
	if commandErr != nil {
		_, _ = service.gitBytes(ctx, discovered.RepositoryRoot, "worktree", "remove", "--force", worktree)
		_ = lock.Close()
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, commandErr
	}
	identity := ownership.Identity
	policy := candidatePolicy{scope: request.AllowedScope, allowed: map[fix.RepoPath]bool{}}
	for _, path := range ownership.Allowed {
		policy.allowed[path] = true
	}
	if err := writeOwnership(jobRoot, ownership); err != nil {
		_, _ = service.gitBytes(ctx, worktree, "worktree", "remove", "--force", worktree)
		_ = lock.Close()
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, fmt.Errorf("persist candidate ownership: %w", err)
	}
	if err := lock.Close(); err != nil {
		cleanupLock, lockErr := acquireRepositoryLock(discovered.GitCommonDir)
		if lockErr == nil {
			_, _ = service.gitBytes(ctx, discovered.RepositoryRoot, "worktree", "remove", "--force", worktree)
			lockErr = cleanupLock.Close()
		}
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, errors.Join(err, lockErr)
	}
	service.mu.Lock()
	service.policies[request.Job] = policy
	service.mu.Unlock()
	retained = false
	return identity, nil
}

// DiscoverPrepared returns only a candidate whose durable ownership marker,
// repository identity, base commit, targets, and frozen path policy exactly
// match the request. It never creates or removes a worktree.
func (service *GitWorktreeService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if !validJobID(request.Job) {
		return fix.CandidateIdentity{}, false, errors.New("discover prepared candidate: invalid job ID")
	}
	jobRoot := filepath.Join(service.stateRoot, string(request.Job))
	if _, err := os.Lstat(jobRoot); errors.Is(err, os.ErrNotExist) {
		return fix.CandidateIdentity{}, false, nil
	} else if err != nil {
		return fix.CandidateIdentity{}, false, fmt.Errorf("inspect candidate state: %w", err)
	}
	want, err := service.expectedOwnership(request)
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	record, err := readOwnership(jobRoot)
	if errors.Is(err, os.ErrNotExist) {
		reservation, reservationErr := readCandidateRecord(jobRoot, reservationName)
		if reservationErr != nil || !sameOwnership(reservation, want) {
			return fix.CandidateIdentity{}, false, errors.New("candidate state exists without a matching durable reservation")
		}
		if _, statErr := os.Lstat(want.Identity.RepositoryRoot); errors.Is(statErr, os.ErrNotExist) {
			return fix.CandidateIdentity{}, false, nil
		} else if statErr != nil {
			return fix.CandidateIdentity{}, false, fmt.Errorf("inspect reserved candidate worktree: %w", statErr)
		}
		if err := service.verifyReservedWorktree(ctx, want.Identity); err != nil {
			return fix.CandidateIdentity{}, false, err
		}
		if err := writeOwnership(jobRoot, want); err != nil {
			return fix.CandidateIdentity{}, false, fmt.Errorf("promote candidate reservation: %w", err)
		}
		record = want
	} else if err != nil {
		return fix.CandidateIdentity{}, false, fmt.Errorf("read existing candidate ownership: %w", err)
	}
	if !sameOwnership(record, want) {
		return fix.CandidateIdentity{}, false, errors.New("existing candidate ownership conflicts with requested workspace or policy")
	}
	identity := record.Identity
	if err := service.Recover(ctx, identity, request.Targets, request.AllowedScope, request.AllowedPaths); err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	return identity, true, nil
}

func (service *GitWorktreeService) expectedOwnership(request PrepareRequest) (ownershipRecord, error) {
	analysisRoot, err := filepath.EvalSymlinks(request.Workspace.AnalysisRoot)
	if err != nil {
		return ownershipRecord{}, fmt.Errorf("canonicalize analysis root: %w", err)
	}
	analysisRelative, err := filepath.Rel(request.Workspace.RepositoryRoot, analysisRoot)
	if err != nil || analysisRelative == ".." || strings.HasPrefix(analysisRelative, ".."+string(filepath.Separator)) {
		return ownershipRecord{}, errors.New("candidate analysis root is outside repository")
	}
	worktree := filepath.Join(service.stateRoot, string(request.Job), "worktree")
	candidateAnalysisRoot := worktree
	if analysisRelative != "." {
		candidateAnalysisRoot = filepath.Join(worktree, analysisRelative)
	}
	targets := append([]fix.RepoPath(nil), request.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	allowed := append([]fix.RepoPath(nil), request.AllowedPaths...)
	if request.AllowedScope != "repository" && len(allowed) == 0 {
		allowed = append(allowed, targets...)
	}
	sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
	identity := fix.CandidateIdentity{Job: request.Job, Repository: request.Workspace.Repository, RepositoryRoot: worktree,
		AnalysisRoot: candidateAnalysisRoot, GitCommonDir: request.Workspace.GitCommonDir, BaseCommit: request.Workspace.BaseCommit}
	return ownershipRecord{Version: 1, Identity: identity, Targets: targets, Allowed: allowed, Scope: request.AllowedScope}, nil
}

func (service *GitWorktreeService) verifyReservedWorktree(ctx context.Context, identity fix.CandidateIdentity) error {
	common, err := service.gitText(ctx, identity.RepositoryRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("verify reserved candidate Git directory: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil || common != identity.GitCommonDir {
		return errors.New("reserved candidate Git common directory does not match")
	}
	head, err := service.gitText(ctx, identity.RepositoryRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != identity.BaseCommit {
		return errors.New("reserved candidate HEAD does not match its admitted base commit")
	}
	return nil
}

func sameOwnership(left, right ownershipRecord) bool {
	if left.Version != right.Version || left.Identity != right.Identity || left.Scope != right.Scope ||
		len(left.Targets) != len(right.Targets) || len(left.Allowed) != len(right.Allowed) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	for index := range left.Allowed {
		if left.Allowed[index] != right.Allowed[index] {
			return false
		}
	}
	return true
}

func (service *GitWorktreeService) Diff(ctx context.Context, identity fix.CandidateIdentity) (DiffSnapshot, error) {
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return DiffSnapshot{}, err
	}
	data, err := service.gitBytes(ctx, identity.RepositoryRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return DiffSnapshot{}, err
	}
	manifest, err := gitmanifest.Build(identity.RepositoryRoot, data)
	if err != nil {
		return DiffSnapshot{}, err
	}
	scope := fix.ScopeClean
	files := make([]DiffFile, 0, len(manifest.Entries))
	for _, changed := range manifest.Entries {
		if !policy.allows(changed.Path) || changed.Previous != "" && !policy.allows(changed.Previous) {
			scope = fix.ScopeViolated
		}
		files = append(files, DiffFile{Path: changed.Path, Previous: changed.Previous, Status: changed.Status, Mode: changed.Mode, Kind: changed.Kind, DiffHash: changed.Hash})
	}
	return DiffSnapshot{Files: files, Fingerprint: manifest.Fingerprint, Scope: scope}, nil
}

func (policy candidatePolicy) allows(path fix.RepoPath) bool {
	if policy.scope == "repository" {
		return true
	}
	if policy.allowed[path] {
		return true
	}
	if policy.scope != "targets-and-tests" {
		return false
	}
	return false
}

func (service *GitWorktreeService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath) (File, error) {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return File{}, err
	}
	if _, err := fix.ParseRepoPath(path.String()); err != nil {
		return File{}, err
	}
	full := filepath.Join(identity.RepositoryRoot, filepath.FromSlash(path.String()))
	canonical, err := filepath.EvalSymlinks(full)
	if err != nil {
		return File{}, err
	}
	if !within(identity.RepositoryRoot, canonical) {
		return File{}, errors.New("candidate file escapes worktree")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("candidate file is not regular")
	}
	if info.Size() > MaxReadFileBytes {
		return File{}, ErrFileTooLarge
	}
	opened, err := os.Open(canonical)
	if err != nil {
		return File{}, err
	}
	defer opened.Close()
	contents, err := io.ReadAll(io.LimitReader(opened, MaxReadFileBytes+1))
	if err != nil {
		return File{}, err
	}
	if len(contents) > MaxReadFileBytes {
		return File{}, ErrFileTooLarge
	}
	hash := sha256.Sum256(contents)
	return File{Path: path, Contents: contents, ContentHash: hex.EncodeToString(hash[:]), Mode: uint32(info.Mode().Perm())}, nil
}

func (service *GitWorktreeService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return err
	}
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return err
	}
	_, commandErr := service.gitBytes(ctx, identity.RepositoryRoot, "worktree", "remove", "--force", identity.RepositoryRoot)
	lockErr := lock.Close()
	if commandErr == nil {
		jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
		commandErr = os.RemoveAll(jobRoot)
	}
	service.mu.Lock()
	delete(service.policies, identity.Job)
	service.mu.Unlock()
	if commandErr == nil {
		commandErr = service.releaseRepository(identity.GitCommonDir, identity.Job)
	}
	return errors.Join(commandErr, lockErr)
}

func (service *GitWorktreeService) ReconcileDiscard(ctx context.Context, identity fix.CandidateIdentity) error {
	if !validJobID(identity.Job) {
		return errors.New("candidate identity has an invalid job ID")
	}
	jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
	expected := filepath.Join(jobRoot, "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return errors.New("candidate identity does not name its exact managed worktree")
	}
	if _, err := os.Lstat(jobRoot); errors.Is(err, os.ErrNotExist) {
		service.mu.Lock()
		delete(service.policies, identity.Job)
		service.mu.Unlock()
		_ = service.releaseRepository(identity.GitCommonDir, identity.Job)
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect candidate discard state: %w", err)
	}
	record, err := readOwnership(jobRoot)
	if err != nil || record.Version != 1 || record.Identity != identity {
		return errors.New("candidate discard ownership marker does not match journal identity")
	}
	if err := service.retainRepository(identity.GitCommonDir, identity.Job); err != nil {
		return err
	}
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return err
	}
	var commandErr error
	if _, statErr := os.Lstat(expected); statErr == nil {
		_, commandErr = service.gitBytes(ctx, expected, "worktree", "remove", "--force", expected)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		commandErr = statErr
	}
	lockErr := lock.Close()
	if commandErr == nil && lockErr == nil {
		commandErr = os.RemoveAll(jobRoot)
	}
	service.mu.Lock()
	delete(service.policies, identity.Job)
	service.mu.Unlock()
	if commandErr == nil && lockErr == nil {
		commandErr = service.releaseRepository(identity.GitCommonDir, identity.Job)
	}
	return errors.Join(commandErr, lockErr)
}

func (service *GitWorktreeService) validateIdentity(ctx context.Context, identity fix.CandidateIdentity) (candidatePolicy, error) {
	if !validJobID(identity.Job) {
		return candidatePolicy{}, errors.New("candidate identity has an invalid job ID")
	}
	expected := filepath.Join(service.stateRoot, string(identity.Job), "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return candidatePolicy{}, errors.New("candidate identity does not name its exact managed worktree")
	}
	record, err := readOwnership(filepath.Dir(expected))
	if err != nil {
		return candidatePolicy{}, fmt.Errorf("read candidate ownership: %w", err)
	}
	if record.Version != 1 || record.Identity != identity {
		return candidatePolicy{}, errors.New("candidate identity does not match its ownership record")
	}
	service.mu.Lock()
	leasedCommon, leased := service.jobLeases[identity.Job]
	service.mu.Unlock()
	if !leased || leasedCommon != identity.GitCommonDir {
		return candidatePolicy{}, errors.New("candidate repository ownership lease is not held")
	}
	common, err := service.gitText(ctx, expected, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return candidatePolicy{}, fmt.Errorf("verify candidate Git directory: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil || common != identity.GitCommonDir {
		return candidatePolicy{}, errors.New("candidate Git common directory does not match ownership")
	}
	head, err := service.gitText(ctx, expected, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || fix.ObjectID(head) != identity.BaseCommit {
		return candidatePolicy{}, errors.New("candidate HEAD moved from its owned base commit")
	}
	policy := candidatePolicy{scope: record.Scope, allowed: make(map[fix.RepoPath]bool, len(record.Allowed))}
	for _, target := range record.Allowed {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return candidatePolicy{}, errors.New("candidate ownership contains an invalid target")
		}
		policy.allowed[target] = true
	}
	service.mu.Lock()
	service.policies[identity.Job] = policy
	service.mu.Unlock()
	return policy, nil
}

// Recover re-establishes an owned candidate after process restart. Durable
// marker policy and journal policy must agree; neither source is trusted alone.
func (service *GitWorktreeService) Recover(ctx context.Context, identity fix.CandidateIdentity, targets []fix.RepoPath, scope string, allowed []fix.RepoPath) error {
	if !validJobID(identity.Job) {
		return errors.New("candidate identity has an invalid job ID")
	}
	expected := filepath.Join(service.stateRoot, string(identity.Job), "worktree")
	if identity.RepositoryRoot != expected || !within(expected, identity.AnalysisRoot) {
		return errors.New("candidate identity does not name its exact managed worktree")
	}
	record, err := readOwnership(filepath.Dir(expected))
	if err != nil || record.Version != 1 || record.Identity != identity {
		return errors.New("candidate identity does not match its ownership record")
	}
	wantTargets := append([]fix.RepoPath(nil), targets...)
	sort.Slice(wantTargets, func(i, j int) bool { return wantTargets[i] < wantTargets[j] })
	if len(record.Targets) != len(wantTargets) {
		return errors.New("candidate journal targets do not match its ownership record")
	}
	for index := range wantTargets {
		if record.Targets[index] != wantTargets[index] {
			return errors.New("candidate journal targets do not match its ownership record")
		}
	}
	if err := service.retainRepository(record.Identity.GitCommonDir, identity.Job); err != nil {
		return err
	}
	recovered := false
	defer func() {
		if !recovered {
			_ = service.releaseRepository(record.Identity.GitCommonDir, identity.Job)
		}
	}()
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return err
	}
	if scope != "repository" && len(allowed) == 0 {
		allowed = wantTargets
	}
	want := candidatePolicy{scope: scope, allowed: make(map[fix.RepoPath]bool, len(allowed))}
	for _, target := range allowed {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return err
		}
		want.allowed[target] = true
	}
	if policy.scope != want.scope || len(policy.allowed) != len(want.allowed) {
		return errors.New("candidate journal policy does not match its ownership record")
	}
	for target := range want.allowed {
		if !policy.allowed[target] {
			return errors.New("candidate journal targets do not match its ownership record")
		}
	}
	recovered = true
	return nil
}

func (service *GitWorktreeService) retainRepository(common string, job fix.JobID) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return errors.New("candidate service is closed")
	}
	if existing, ok := service.jobLeases[job]; ok {
		if existing != common {
			return errors.New("job already owns a different repository lease")
		}
		return nil
	}
	owned := service.leases[common]
	if owned == nil {
		lease, err := acquireRepositoryOwnership(common)
		if err != nil {
			return err
		}
		owned = &repositoryOwnership{lease: lease, jobs: map[fix.JobID]bool{}}
		service.leases[common] = owned
	}
	owned.jobs[job] = true
	service.jobLeases[job] = common
	return nil
}

func (service *GitWorktreeService) releaseRepository(common string, job fix.JobID) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.jobLeases[job] != common {
		return nil
	}
	delete(service.jobLeases, job)
	owned := service.leases[common]
	if owned == nil {
		return nil
	}
	delete(owned.jobs, job)
	if len(owned.jobs) != 0 {
		return nil
	}
	delete(service.leases, common)
	return owned.lease.Close()
}

func (service *GitWorktreeService) Close() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return nil
	}
	service.closed = true
	var result error
	for common, owned := range service.leases {
		result = errors.Join(result, owned.lease.Close())
		delete(service.leases, common)
	}
	clear(service.jobLeases)
	return result
}

func validJobID(job fix.JobID) bool {
	value := string(job)
	if len(value) != len("job-")+32 || !strings.HasPrefix(value, "job-") {
		return false
	}
	_, err := hex.DecodeString(value[len("job-"):])
	return err == nil
}

func writeOwnership(jobRoot string, record ownershipRecord) error {
	return writeCandidateRecord(jobRoot, ownershipName, record)
}

func ensureCandidateRecord(jobRoot, name string, record ownershipRecord) error {
	existing, err := readCandidateRecord(jobRoot, name)
	if err == nil {
		if !sameOwnership(existing, record) {
			return errors.New("existing candidate marker conflicts with admitted ownership")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeCandidateRecord(jobRoot, name, record)
}

func writeCandidateRecord(jobRoot, name string, record ownershipRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(jobRoot, "."+name+"-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, filepath.Join(jobRoot, name)); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(jobRoot)
}

func readOwnership(jobRoot string) (ownershipRecord, error) {
	return readCandidateRecord(jobRoot, ownershipName)
}

func readCandidateRecord(jobRoot, name string) (ownershipRecord, error) {
	path := filepath.Join(jobRoot, name)
	data, err := readPrivateRegular(path)
	if err != nil {
		return ownershipRecord{}, err
	}
	var record ownershipRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ownershipRecord{}, err
	}
	return record, nil
}

func (service *GitWorktreeService) gitText(ctx context.Context, directory string, arguments ...string) (string, error) {
	data, err := service.gitBytes(ctx, directory, arguments...)
	return strings.TrimSpace(string(data)), err
}

func (service *GitWorktreeService) gitBytes(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	return service.gitBytesWithInputAndExitOne(ctx, directory, nil, false, arguments...)
}

func (service *GitWorktreeService) gitBytesInput(ctx context.Context, directory string, input []byte, arguments ...string) ([]byte, error) {
	return service.gitBytesWithInputAndExitOne(ctx, directory, input, false, arguments...)
}

func (service *GitWorktreeService) gitBytesAllowExitOne(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	return service.gitBytesWithInputAndExitOne(ctx, directory, nil, true, arguments...)
}

func (service *GitWorktreeService) gitBytesWithInputAndExitOne(ctx context.Context, directory string, input []byte, exitOneOK bool, arguments ...string) ([]byte, error) {
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.git, Arguments: trusted, Directory: directory,
		Environment: []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"},
		Stdin:       input,
		Limits:      isolation.Limits{WallTime: 2 * time.Minute, TerminateGrace: 2 * time.Second, MaxStdoutBytes: gitOutputLimit, MaxStderrBytes: gitOutputLimit}})
	if err != nil {
		return nil, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("Git output exceeded safety limit")
	}
	if !result.Successful() && !(exitOneOK && result.ExitCode == 1) {
		return nil, fmt.Errorf("git %s failed with exit %d: %s", arguments[0], result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
