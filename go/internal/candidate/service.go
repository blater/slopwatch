// Package candidate owns isolated remediation workspaces and their diff/file
// inventory. It does not own agent execution or Git publication.
package candidate

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

const MaxReadFileBytes = 4 << 20

var ErrFileTooLarge = errors.New("candidate file exceeds monitor read limit")

type PreflightRequest struct {
	Workspace fix.WorkspaceIdentity
	Targets   []fix.RepoPath
}

type PreflightResult struct {
	Clean        bool
	Supported    bool
	Diagnostic   string
	CheckedAt    time.Time
	TargetBlobs  map[fix.RepoPath]fix.ObjectID
	AllowedPaths []fix.RepoPath
}

// ScopePlanner freezes the exact repository-relative paths admitted by a
// named change scope before a job is submitted.
type ScopePlanner interface {
	Plan(context.Context, fix.WorkspaceIdentity, []fix.RepoPath, string) ([]fix.RepoPath, error)
}

type PrepareRequest struct {
	Job       fix.JobID
	Workspace fix.WorkspaceIdentity
	Targets   []fix.RepoPath
	// AllowedScope is a closed policy name such as "targets" or
	// "targets-and-tests". It is interpreted by the trusted candidate service,
	// never by an agent adapter.
	AllowedScope string
	AllowedPaths []fix.RepoPath
}

type DiffFile struct {
	Path      fix.RepoPath
	Previous  fix.RepoPath
	Status    string
	Mode      uint32
	Kind      string
	Additions int
	Deletions int
	Binary    bool
	DiffHash  string
}

type DiffSnapshot struct {
	Files       []DiffFile
	Fingerprint string
	Scope       fix.ScopeState
	Diagnostic  string
}

type File struct {
	Path        fix.RepoPath
	Contents    []byte
	ContentHash string
	Mode        uint32
}

type Service interface {
	Preflight(context.Context, PreflightRequest) (PreflightResult, error)
	Prepare(context.Context, PrepareRequest) (fix.CandidateIdentity, error)
	DiscoverPrepared(context.Context, PrepareRequest) (fix.CandidateIdentity, bool, error)
	Diff(context.Context, fix.CandidateIdentity) (DiffSnapshot, error)
	ReadFile(context.Context, fix.CandidateIdentity, fix.RepoPath) (File, error)
	Recover(context.Context, fix.CandidateIdentity, []fix.RepoPath, string, []fix.RepoPath) error
	// ReconcileDiscard completes an interrupted discard using the durable
	// ownership marker, or confirms that the exact owned candidate is gone.
	ReconcileDiscard(context.Context, fix.CandidateIdentity) error
	Discard(context.Context, fix.CandidateIdentity) error
	Close() error
}
