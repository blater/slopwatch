package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

type GitWorktreeConfig struct {
	DiscoveryCommandOutputBytes int64
}

// GitWorktreeService owns detached per-job worktrees. It is the only
// remediation component allowed to inspect Git metadata; agent adapters only
// receive the resulting candidate root through agent.Request.
type GitWorktreeService struct {
	stateRoot                   string
	executor                    gitExecutor
	registry                    candidateRegistry
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
	return &GitWorktreeService{stateRoot: stateRoot, executor: gitExecutor{git: git, runner: runner}, registry: newCandidateRegistry(), discoveryCommandOutputBytes: config.DiscoveryCommandOutputBytes}, nil
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
	top, err := service.executor.text(ctx, absolute, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover repository root: %w", err)
	}
	common, err := service.executor.text(ctx, top, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover git common directory: %w", err)
	}
	commit, err := service.executor.text(ctx, top, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fix.WorkspaceIdentity{}, fmt.Errorf("discover base commit: %w", err)
	}
	branch, _ := service.executor.text(ctx, top, "symbolic-ref", "--quiet", "--short", "HEAD")
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

func (service *GitWorktreeService) Close() error { return service.registry.close() }
