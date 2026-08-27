// Package candidate owns isolated remediation workspaces and their diff/file
// inventory. It does not own agent execution or Git publication.
package candidate

import (
	"context"

	"github.com/blater/slopmochi/internal/fix"
)

// ScopePlanner freezes the exact repository-relative paths admitted by a
// named change scope before a job is submitted.
type ScopePlanner interface {
	Plan(context.Context, fix.WorkspaceIdentity, []fix.RepoPath, string) ([]fix.RepoPath, error)
}

type PrepareRequest struct {
	Job       fix.JobID
	Workspace fix.WorkspaceIdentity
	Mode      fix.WorkspaceMode
	Targets   []fix.RepoPath
	// AllowedScope is a closed policy name such as "targets" or
	// "targets-and-tests". It is interpreted by the trusted candidate service,
	// never by an agent adapter.
	AllowedScope       string
	AllowedPaths       []fix.RepoPath
	CommandOutputBytes int64
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
	Truncated   bool
}

type Service interface {
	Prepare(context.Context, PrepareRequest) (fix.CandidateIdentity, error)
	DiscoverPrepared(context.Context, PrepareRequest) (fix.CandidateIdentity, bool, error)
	Diff(context.Context, fix.CandidateIdentity) (DiffSnapshot, error)
	ReadFile(context.Context, fix.CandidateIdentity, fix.RepoPath, int64) (File, error)
	Recover(context.Context, fix.CandidateIdentity, []fix.RepoPath, string, []fix.RepoPath) error
	// ReconcileDiscard completes an interrupted discard using the durable
	// ownership marker, or confirms that the exact owned candidate is gone.
	ReconcileDiscard(context.Context, fix.CandidateIdentity) error
	Discard(context.Context, fix.CandidateIdentity) error
	// Release ends Slopmochi ownership without deleting a preserved workspace.
	Release(context.Context, fix.CandidateIdentity) error
	Close() error
}
