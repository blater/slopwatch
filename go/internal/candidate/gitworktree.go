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

	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/gitmanifest"
	"github.com/blater/slopmochi/internal/isolation"
)

type GitWorktreeConfig struct {
	DiscoveryCommandOutputBytes int64
}

type commandOutputContextKey struct{}

// GitWorktreeService owns detached per-job worktrees. It is the only
// remediation component allowed to inspect Git metadata; agent adapters only
// receive the resulting candidate root through agent.Request.
type GitWorktreeService struct {
	stateRoot                   string
	git                         string
	runner                      isolation.Executor
	mu                          sync.Mutex
	policies                    map[fix.JobID]candidatePolicy
	leases                      map[string]*repositoryOwnership
	jobLeases                   map[fix.JobID]string
	closed                      bool
	discoveryCommandOutputBytes int64
}

type repositoryOwnership struct {
	lease *repositoryLock
	jobs  map[fix.JobID]bool
}

type candidatePolicy struct {
	allowed            map[fix.RepoPath]bool
	scope              string
	commandOutputBytes int64
}

const (
	ownershipName     = "owner.json"
	reservationName   = "reservation.json"
	seedManifestName  = "seed-manifest.json"
	seedCompletedName = "seed-completed.json"
	maxSeedByteLimit  = int64(^uint64(0) >> 1)
)

type ownershipRecord struct {
	Version            int                   `json:"version"`
	Identity           fix.CandidateIdentity `json:"identity"`
	Targets            []fix.RepoPath        `json:"targets"`
	Allowed            []fix.RepoPath        `json:"allowed"`
	Scope              string                `json:"scope"`
	CommandOutputBytes int64                 `json:"command_output_bytes"`
}

type seedManifest struct {
	Version int         `json:"version"`
	Entries []seedEntry `json:"entries"`
}

type seedEntry struct {
	Path    fix.RepoPath `json:"path"`
	Staged  string       `json:"staged,omitempty"`
	Deleted bool         `json:"deleted,omitempty"`
	Mode    uint32       `json:"mode,omitempty"`
	Size    int64        `json:"size,omitempty"`
	Hash    string       `json:"sha256,omitempty"`
}

type seedSpec struct {
	path          fix.RepoPath
	forceDeletion bool
}

type seedCompletion struct {
	Version      int    `json:"version"`
	ManifestHash string `json:"manifest_sha256"`
}

func NewGitWorktreeService(stateRoot string, runner isolation.Executor, config GitWorktreeConfig) (*GitWorktreeService, error) {
	if stateRoot == "" || !filepath.IsAbs(stateRoot) || runner == nil || config.DiscoveryCommandOutputBytes <= 0 {
		return nil, errors.New("candidate worktree service requires an absolute private state root, process runner, and positive discovery command output budget")
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
	return &GitWorktreeService{stateRoot: stateRoot, git: git, runner: runner, discoveryCommandOutputBytes: config.DiscoveryCommandOutputBytes, policies: map[fix.JobID]candidatePolicy{}, leases: map[string]*repositoryOwnership{}, jobLeases: map[fix.JobID]string{}}, nil
}

// DiscoverWorkspace resolves all security-sensitive repository paths via Git
// rather than trusting a cache entry or a caller-composed .git path.
func (service *GitWorktreeService) DiscoverWorkspace(ctx context.Context, root string) (fix.WorkspaceIdentity, error) {
	if commandOutputBytes(ctx) <= 0 {
		ctx = withCommandOutputBytes(ctx, service.discoveryCommandOutputBytes)
	}
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
	branch, _ := service.gitText(ctx, top, "symbolic-ref", "--quiet", "--short", "HEAD")
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("canonicalize repository root: %w", err)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("canonicalize git common directory: %w", err)
	}
	hash := sha256.Sum256([]byte(top + "\x00" + common))
	return fix.WorkspaceIdentity{Repository: fix.RepositoryID(hex.EncodeToString(hash[:16])), RepositoryRoot: top, AnalysisRoot: top, GitCommonDir: common, BaseCommit: fix.ObjectID(commit), CurrentBranch: branch}, nil
}

func (service *GitWorktreeService) Prepare(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, error) {
	if !validJobID(request.Job) || len(request.Targets) == 0 || request.CommandOutputBytes <= 0 {
		return fix.CandidateIdentity{}, errors.New("prepare candidate: job and targets are required")
	}
	ctx = withCommandOutputBytes(ctx, request.CommandOutputBytes)
	if existing, found, err := service.DiscoverPrepared(ctx, request); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("reconcile prepared candidate: %w", err)
	} else if found {
		return existing, nil
	}
	discovered, err := service.DiscoverWorkspace(ctx, request.Workspace.RepositoryRoot)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	// Do not predict whether Git policy or repository shape will permit the
	// operation. Attempt candidate creation and report the concrete failing
	// command instead. Repository identity is the exception: continuing after
	// the path resolves to a different repository could act on the wrong Git
	// metadata and is a true technical safety boundary.
	if request.Workspace.Repository != "" && request.Workspace.Repository != discovered.Repository ||
		request.Workspace.GitCommonDir != "" && request.Workspace.GitCommonDir != discovered.GitCommonDir {
		return fix.CandidateIdentity{}, errors.New("repository identity changed before candidate creation")
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
	if err := os.Mkdir(filepath.Join(jobRoot, "staging"), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fix.CandidateIdentity{}, fmt.Errorf("create candidate staging directory: %w", err)
	}
	if err := ensureCandidateRecord(jobRoot, reservationName, ownership); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("persist candidate reservation: %w", err)
	}
	manifest, err := service.snapshotWorkingChanges(ctx, discovered.RepositoryRoot, ownership.Identity.StagingRoot,
		request.AllowedScope, request.AllowedPaths)
	if err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, err
	}
	manifestHash, err := writeSeedManifest(jobRoot, manifest)
	if err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, fmt.Errorf("persist workspace snapshot: %w", err)
	}
	lock, err := acquireRepositoryLock(discovered.GitCommonDir)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	_, commandErr := service.gitBytes(ctx, discovered.RepositoryRoot, "worktree", "add", "--detach", "--no-checkout", worktree, string(discovered.BaseCommit))
	if commandErr == nil {
		_, commandErr = service.gitBytes(ctx, worktree, "checkout", "--detach", string(discovered.BaseCommit), "--")
	}
	if commandErr == nil {
		commandErr = applySeedManifest(ctx, ownership.Identity.StagingRoot, worktree, manifest)
	}
	if commandErr == nil {
		commandErr = writeSeedCompletion(jobRoot, seedCompletion{Version: 1, ManifestHash: manifestHash})
	}
	if commandErr != nil {
		// Cancellation stops the operation that was in flight, but must not also
		// cancel exact cleanup of a worktree Git may already have registered.
		// Preserve the durable reservation if cleanup fails so Retry/restart can
		// reconcile it instead of orphaning unowned Git metadata.
		cleanupCtx := withCommandOutputBytes(context.Background(), request.CommandOutputBytes)
		_, cleanupErr := service.gitBytes(cleanupCtx, discovered.RepositoryRoot, "worktree", "remove", "--force", worktree)
		_ = lock.Close()
		if cleanupErr == nil {
			cleanupErr = os.RemoveAll(jobRoot)
		}
		return fix.CandidateIdentity{}, errors.Join(commandErr, cleanupErr)
	}
	identity := ownership.Identity
	policy := candidatePolicy{scope: request.AllowedScope, allowed: map[fix.RepoPath]bool{}, commandOutputBytes: request.CommandOutputBytes}
	for _, path := range ownership.Allowed {
		policy.allowed[path] = true
	}
	if err := writeOwnership(jobRoot, ownership); err != nil {
		cleanupCtx := withCommandOutputBytes(context.Background(), request.CommandOutputBytes)
		_, cleanupErr := service.gitBytes(cleanupCtx, discovered.RepositoryRoot, "worktree", "remove", "--force", worktree)
		_ = lock.Close()
		if cleanupErr == nil {
			cleanupErr = os.RemoveAll(jobRoot)
		}
		return fix.CandidateIdentity{}, errors.Join(fmt.Errorf("persist candidate ownership: %w", err), cleanupErr)
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

func (service *GitWorktreeService) snapshotWorkingChanges(ctx context.Context, source, staging, scope string, allowed []fix.RepoPath) (seedManifest, error) {
	status, err := service.gitBytes(ctx, source, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return seedManifest{}, fmt.Errorf("list current workspace changes: %w", err)
	}
	allowedSet := make(map[fix.RepoPath]bool, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = true
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return seedManifest{}, fmt.Errorf("open source workspace: %w", err)
	}
	defer sourceRoot.Close()
	stagingRoot, err := os.OpenRoot(staging)
	if err != nil {
		return seedManifest{}, fmt.Errorf("open candidate staging area: %w", err)
	}
	defer stagingRoot.Close()
	specs := map[fix.RepoPath]seedSpec{}
	entries := bytes.Split(status, []byte{0})
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		statusCode := string(entry[:2])
		path, parseErr := fix.ParseRepoPath(string(entry[3:]))
		if parseErr != nil {
			return seedManifest{}, fmt.Errorf("current workspace contains an unsupported changed path: %w", parseErr)
		}
		var original fix.RepoPath
		if strings.ContainsAny(statusCode, "RC") && index+1 < len(entries) {
			index++
			original, parseErr = fix.ParseRepoPath(string(entries[index]))
			if parseErr != nil {
				return seedManifest{}, fmt.Errorf("current workspace contains an unsupported rename/copy source: %w", parseErr)
			}
		}
		pathAllowed := scope == "repository" || allowedSet[path]
		originalAllowed := original == "" || scope == "repository" || allowedSet[original]
		if original != "" && pathAllowed != originalAllowed {
			return seedManifest{}, fmt.Errorf("current workspace rename/copy crosses the allowed scope: %s and %s", original, path)
		}
		if !pathAllowed {
			continue
		}
		specs[path] = seedSpec{path: path}
		if original != "" {
			specs[original] = seedSpec{path: original, forceDeletion: strings.Contains(statusCode, "R")}
		}
	}
	paths := make([]fix.RepoPath, 0, len(specs))
	for path := range specs {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	manifest := seedManifest{Version: 1, Entries: make([]seedEntry, 0, len(paths))}
	for index, path := range paths {
		entry, err := snapshotWorkingFile(ctx, sourceRoot, stagingRoot, specs[path], index)
		if err != nil {
			return seedManifest{}, err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	return manifest, nil
}

func snapshotWorkingFile(ctx context.Context, source, staging *os.Root, spec seedSpec, index int) (seedEntry, error) {
	if err := ctx.Err(); err != nil {
		return seedEntry{}, err
	}
	name := spec.path.String()
	if spec.forceDeletion {
		return seedEntry{Path: spec.path, Deleted: true}, nil
	}
	info, err := source.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return seedEntry{Path: spec.path, Deleted: true}, nil
	}
	if err != nil {
		return seedEntry{}, fmt.Errorf("inspect changed workspace file %s: %w", spec.path, err)
	}
	if !info.Mode().IsRegular() {
		return seedEntry{}, fmt.Errorf("changed workspace path %s is not a regular file", spec.path)
	}
	input, err := source.Open(name)
	if err != nil {
		return seedEntry{}, fmt.Errorf("open changed workspace file %s: %w", spec.path, err)
	}
	defer input.Close()
	staged := fmt.Sprintf("blob-%06d", index)
	output, err := staging.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return seedEntry{}, fmt.Errorf("stage changed workspace file %s: %w", spec.path, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return seedEntry{}, fmt.Errorf("snapshot changed workspace file %s: %w", spec.path, errors.Join(copyErr, syncErr, closeErr))
	}
	return seedEntry{Path: spec.path, Staged: staged, Mode: uint32(info.Mode().Perm()), Size: written, Hash: hex.EncodeToString(hash.Sum(nil))}, ctx.Err()
}

func applySeedManifest(ctx context.Context, staging, destination string, manifest seedManifest) error {
	stagingRoot, err := os.OpenRoot(staging)
	if err != nil {
		return fmt.Errorf("open candidate staging area: %w", err)
	}
	defer stagingRoot.Close()
	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open isolated worktree: %w", err)
	}
	defer destinationRoot.Close()
	for _, entry := range manifest.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Path.String()
		if entry.Deleted {
			if err := destinationRoot.RemoveAll(name); err != nil {
				return fmt.Errorf("apply deleted workspace file %s: %w", entry.Path, err)
			}
			continue
		}
		parent := filepath.Dir(name)
		if parent != "." {
			if err := destinationRoot.MkdirAll(parent, 0o700); err != nil {
				return fmt.Errorf("create isolated parent for %s: %w", entry.Path, err)
			}
		}
		if existing, statErr := destinationRoot.Lstat(name); statErr == nil && existing.Mode()&os.ModeSymlink != 0 {
			if err := destinationRoot.Remove(name); err != nil {
				return fmt.Errorf("replace isolated symlink %s: %w", entry.Path, err)
			}
		}
		input, err := stagingRoot.Open(entry.Staged)
		if err != nil {
			return fmt.Errorf("open staged workspace file %s: %w", entry.Path, err)
		}
		output, err := destinationRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(entry.Mode))
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("create isolated workspace file %s: %w", entry.Path, err)
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, entry.Size+1))
		if written != entry.Size {
			copyErr = errors.Join(copyErr, fmt.Errorf("staged size changed from %d to %d", entry.Size, written))
		}
		chmodErr := output.Chmod(os.FileMode(entry.Mode))
		syncErr := output.Sync()
		closeErr := errors.Join(input.Close(), output.Close())
		if err := errors.Join(copyErr, chmodErr, syncErr, closeErr); err != nil {
			return fmt.Errorf("apply workspace snapshot for %s: %w", entry.Path, err)
		}
	}
	if err := syncDirectory(destination); err != nil {
		return fmt.Errorf("sync isolated workspace snapshot: %w", err)
	}
	return verifySeedManifest(destination, manifest)
}

func verifySeedManifest(destination string, manifest seedManifest) error {
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, entry := range manifest.Entries {
		info, err := root.Lstat(entry.Path.String())
		if entry.Deleted {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err == nil {
				return fmt.Errorf("deleted path %s is still present", entry.Path)
			}
			return err
		}
		if err != nil || !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != entry.Mode || info.Size() != entry.Size {
			return fmt.Errorf("workspace snapshot metadata differs for %s", entry.Path)
		}
		file, err := root.Open(entry.Path.String())
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(hash, io.LimitReader(file, entry.Size+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Hash {
			return fmt.Errorf("workspace snapshot content differs for %s", entry.Path)
		}
	}
	return nil
}

// DiscoverPrepared returns only a candidate whose durable ownership marker,
// repository identity, base commit, targets, and frozen path policy exactly
// match the request. It removes only an exact matching reservation proven
// incomplete; it never adopts a partial worktree.
func (service *GitWorktreeService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if !validJobID(request.Job) || request.CommandOutputBytes <= 0 {
		return fix.CandidateIdentity{}, false, errors.New("discover prepared candidate: invalid job ID")
	}
	ctx = withCommandOutputBytes(ctx, request.CommandOutputBytes)
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
			if err := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); err != nil {
				return fix.CandidateIdentity{}, false, fmt.Errorf("remove incomplete candidate reservation: %w", err)
			}
			return fix.CandidateIdentity{}, false, nil
		} else if statErr != nil {
			return fix.CandidateIdentity{}, false, fmt.Errorf("inspect reserved candidate worktree: %w", statErr)
		}
		if err := service.verifyReservedWorktree(ctx, want.Identity); err != nil {
			return fix.CandidateIdentity{}, false, err
		}
		manifest, manifestHash, err := readSeedManifest(jobRoot)
		if err != nil {
			if cleanupErr := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); cleanupErr != nil {
				return fix.CandidateIdentity{}, false, errors.Join(errors.New("candidate reservation has no complete workspace snapshot"), cleanupErr)
			}
			return fix.CandidateIdentity{}, false, nil
		}
		completion, err := readSeedCompletion(jobRoot)
		if err != nil || completion.Version != 1 || completion.ManifestHash != manifestHash {
			if cleanupErr := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); cleanupErr != nil {
				return fix.CandidateIdentity{}, false, errors.Join(errors.New("candidate reservation has no durable completion marker"), cleanupErr)
			}
			return fix.CandidateIdentity{}, false, nil
		}
		if err := verifySeedManifest(want.Identity.RepositoryRoot, manifest); err != nil {
			if cleanupErr := service.discardIncompleteReservation(ctx, request.Workspace.RepositoryRoot, jobRoot, want.Identity); cleanupErr != nil {
				return fix.CandidateIdentity{}, false, errors.Join(fmt.Errorf("verify completed candidate snapshot: %w", err), cleanupErr)
			}
			return fix.CandidateIdentity{}, false, nil
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

func (service *GitWorktreeService) discardIncompleteReservation(ctx context.Context, repositoryRoot, jobRoot string, identity fix.CandidateIdentity) error {
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return fmt.Errorf("lock repository for incomplete candidate cleanup: %w", err)
	}
	_, cleanupErr := service.gitBytes(ctx, repositoryRoot, "worktree", "remove", "--force", identity.RepositoryRoot)
	if cleanupErr != nil {
		listing, listErr := service.gitBytes(ctx, repositoryRoot, "worktree", "list", "--porcelain")
		if listErr == nil && !worktreeRegistered(listing, identity.RepositoryRoot) {
			cleanupErr = nil
		} else {
			cleanupErr = errors.Join(cleanupErr, listErr)
		}
	}
	lockErr := lock.Close()
	if cleanupErr == nil && lockErr == nil {
		cleanupErr = os.RemoveAll(jobRoot)
	}
	return errors.Join(cleanupErr, lockErr)
}

func worktreeRegistered(listing []byte, expected string) bool {
	for _, line := range bytes.Split(listing, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("worktree ")) && filepath.Clean(string(bytes.TrimPrefix(line, []byte("worktree ")))) == expected {
			return true
		}
	}
	return false
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
		WorkspaceMode: fix.WorkspaceWorktree,
		AnalysisRoot:  candidateAnalysisRoot, GitCommonDir: request.Workspace.GitCommonDir, BaseCommit: request.Workspace.BaseCommit,
		StagingRoot: filepath.Join(service.stateRoot, string(request.Job), "staging")}
	return ownershipRecord{Version: 1, Identity: identity, Targets: targets, Allowed: allowed, Scope: request.AllowedScope,
		CommandOutputBytes: request.CommandOutputBytes}, nil
}

func (service *GitWorktreeService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return err
	}
	jobRoot := filepath.Join(service.stateRoot, string(identity.Job))
	for _, name := range []string{ownershipName, reservationName, seedManifestName, seedCompletedName} {
		if err := os.Remove(filepath.Join(jobRoot, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("release preserved worktree: %w", err)
		}
	}
	if err := os.RemoveAll(filepath.Join(jobRoot, "staging")); err != nil {
		return fmt.Errorf("release preserved worktree staging: %w", err)
	}
	service.mu.Lock()
	delete(service.policies, identity.Job)
	service.mu.Unlock()
	return service.releaseRepository(identity.GitCommonDir, identity.Job)
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
	if left.Version != right.Version || left.Identity != right.Identity || left.Scope != right.Scope || left.CommandOutputBytes != right.CommandOutputBytes ||
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
	ctx = withCommandOutputBytes(ctx, policy.commandOutputBytes)
	data, err := service.gitBytes(ctx, identity.RepositoryRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return DiffSnapshot{}, err
	}
	manifest, err := gitmanifest.Build(identity.RepositoryRoot, data)
	if err != nil {
		return DiffSnapshot{}, err
	}
	files := make([]DiffFile, 0, len(manifest.Entries))
	for _, changed := range manifest.Entries {
		files = append(files, DiffFile{Path: changed.Path, Previous: changed.Previous, Status: changed.Status, Mode: changed.Mode, Kind: changed.Kind, DiffHash: changed.Hash})
	}
	return DiffSnapshot{Files: files, Fingerprint: manifest.Fingerprint, Scope: fix.ScopeClean}, nil
}

func (service *GitWorktreeService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath, maximum int64) (File, error) {
	if _, err := service.validateIdentity(ctx, identity); err != nil {
		return File{}, err
	}
	return readCandidateFile(ctx, identity.RepositoryRoot, path, maximum)
}

func (service *GitWorktreeService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	policy, err := service.validateIdentity(ctx, identity)
	if err != nil {
		return err
	}
	ctx = withCommandOutputBytes(ctx, policy.commandOutputBytes)
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
		ctx = withCommandOutputBytes(ctx, service.discoveryCommandOutputBytes)
		lock, lockErr := acquireRepositoryLock(identity.GitCommonDir)
		if lockErr != nil {
			return lockErr
		}
		_, commandErr := service.gitBytes(ctx, identity.GitCommonDir, "worktree", "remove", "--force", expected)
		if commandErr != nil {
			listing, listErr := service.gitBytes(ctx, identity.GitCommonDir, "worktree", "list", "--porcelain")
			if listErr == nil && !worktreeRegistered(listing, expected) {
				commandErr = nil
			} else {
				commandErr = errors.Join(commandErr, listErr)
			}
		}
		lockErr = lock.Close()
		var releaseErr error
		if commandErr == nil && lockErr == nil {
			service.mu.Lock()
			delete(service.policies, identity.Job)
			service.mu.Unlock()
			releaseErr = service.releaseRepository(identity.GitCommonDir, identity.Job)
		}
		return errors.Join(commandErr, lockErr, releaseErr)
	} else if err != nil {
		return fmt.Errorf("inspect candidate discard state: %w", err)
	}
	record, err := readOwnership(jobRoot)
	if err != nil || record.Version != 1 || record.Identity != identity {
		return errors.New("candidate discard ownership marker does not match saved identity")
	}
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
	if err := service.retainRepository(identity.GitCommonDir, identity.Job); err != nil {
		return err
	}
	lock, err := acquireRepositoryLock(identity.GitCommonDir)
	if err != nil {
		return err
	}
	_, commandErr := service.gitBytes(ctx, identity.GitCommonDir, "worktree", "remove", "--force", expected)
	if commandErr != nil {
		listing, listErr := service.gitBytes(ctx, identity.GitCommonDir, "worktree", "list", "--porcelain")
		if listErr == nil && !worktreeRegistered(listing, expected) {
			commandErr = nil
		} else {
			commandErr = errors.Join(commandErr, listErr)
		}
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
	if record.CommandOutputBytes <= 0 {
		return candidatePolicy{}, errors.New("candidate ownership lacks a command output budget")
	}
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
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
	policy := candidatePolicy{scope: record.Scope, allowed: make(map[fix.RepoPath]bool, len(record.Allowed)), commandOutputBytes: record.CommandOutputBytes}
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
// marker policy and saved policy must agree; neither source is trusted alone.
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
	ctx = withCommandOutputBytes(ctx, record.CommandOutputBytes)
	wantTargets := append([]fix.RepoPath(nil), targets...)
	sort.Slice(wantTargets, func(i, j int) bool { return wantTargets[i] < wantTargets[j] })
	if len(record.Targets) != len(wantTargets) {
		return errors.New("saved candidate targets do not match its ownership record")
	}
	for index := range wantTargets {
		if record.Targets[index] != wantTargets[index] {
			return errors.New("saved candidate targets do not match its ownership record")
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
	want := candidatePolicy{scope: scope, allowed: make(map[fix.RepoPath]bool, len(allowed)), commandOutputBytes: record.CommandOutputBytes}
	for _, target := range allowed {
		if _, err := fix.ParseRepoPath(target.String()); err != nil {
			return err
		}
		want.allowed[target] = true
	}
	if policy.scope != want.scope || len(policy.allowed) != len(want.allowed) {
		return errors.New("saved candidate policy does not match its ownership record")
	}
	for target := range want.allowed {
		if !policy.allowed[target] {
			return errors.New("saved candidate targets do not match its ownership record")
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
	if !strings.HasPrefix(value, "job-") {
		return false
	}
	name := strings.TrimPrefix(value, "job-")
	words := strings.Split(name, "-")
	if len(words) != 3 {
		return false
	}
	for _, word := range words {
		if len(word) == 0 || len(word) > 8 {
			return false
		}
		for _, character := range word {
			if character < 'a' || character > 'z' {
				return false
			}
		}
	}
	return true
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
	return writePrivateJSON(jobRoot, name, record)
}

func writeSeedManifest(jobRoot string, manifest seedManifest) (string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), writePrivateData(jobRoot, seedManifestName, data)
}

func writeSeedCompletion(jobRoot string, completion seedCompletion) error {
	return writePrivateJSON(jobRoot, seedCompletedName, completion)
}

func readSeedManifest(jobRoot string) (seedManifest, string, error) {
	data, err := readPrivateRegular(filepath.Join(jobRoot, seedManifestName))
	if err != nil {
		return seedManifest{}, "", err
	}
	var manifest seedManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return seedManifest{}, "", errors.New("candidate seed manifest is invalid")
	}
	for _, entry := range manifest.Entries {
		if entry.Size < 0 || entry.Size >= maxSeedByteLimit {
			return seedManifest{}, "", errors.New("candidate seed manifest has an invalid file size")
		}
	}
	hash := sha256.Sum256(data)
	return manifest, hex.EncodeToString(hash[:]), nil
}

func readSeedCompletion(jobRoot string) (seedCompletion, error) {
	data, err := readPrivateRegular(filepath.Join(jobRoot, seedCompletedName))
	if err != nil {
		return seedCompletion{}, err
	}
	var completion seedCompletion
	if err := json.Unmarshal(data, &completion); err != nil {
		return seedCompletion{}, err
	}
	return completion, nil
}

func writePrivateJSON(jobRoot, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writePrivateData(jobRoot, name, data)
}

func writePrivateData(jobRoot, name string, data []byte) error {
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
	maximum := commandOutputBytes(ctx)
	if maximum <= 0 {
		return nil, errors.New("candidate Git command output budget is not configured")
	}
	trusted := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.autocrlf=false", "-c", "core.filemode=true", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
	trusted = append(trusted, arguments...)
	result, err := service.runner.Run(ctx, isolation.Request{Executable: service.git, Arguments: trusted, Directory: directory,
		Environment: []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"},
		Stdin:       nil,
		Limits:      isolation.Limits{TerminateGrace: 2 * time.Second, MaxStdoutBytes: maximum, MaxStderrBytes: maximum}})
	if err != nil {
		return nil, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("Git output exceeded safety limit")
	}
	if !result.Successful() {
		return nil, fmt.Errorf("git %s failed with exit %d: %s", arguments[0], result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}

func withCommandOutputBytes(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

func commandOutputBytes(ctx context.Context) int64 {
	maximum, _ := ctx.Value(commandOutputContextKey{}).(int64)
	return maximum
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
