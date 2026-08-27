// Package delivery owns local commit/ref creation and exact remote-ref push.
// Agent runtimes never receive this service.
package delivery

import (
	"context"

	"github.com/blater/slopmochi/internal/fix"
)

type Request struct {
	Job                    fix.JobID
	Candidate              fix.CandidateIdentity
	DiffHash               string
	Plan                   fix.DeliveryPlan
	Paths                  []fix.RepoPath
	Branch                 string
	Remote                 string
	CommitTitle            string
	CommitBody             string
	ExpectedRemoteHost     string
	HostRepository         string
	ExpectedRemoteIdentity string
	CommandOutputBytes     int64
}

type Result struct {
	Commit     fix.ObjectID
	LocalRef   string
	RemoteRef  string
	Repository string
	Pushed     bool
	Ambiguous  bool
	Diagnostic string
}

type Service interface {
	PublishCommit(context.Context, Request) (Result, error)
	Reconcile(context.Context, Request, Result) (Result, error)
}

// SagaService exposes publication at the durable acknowledgement boundaries
// used by the fix controller. Each successful return can be saved before
// the next externally visible side effect begins.
type SagaService interface {
	CreateCommit(context.Context, Request) (Result, error)
	CreateLocalRef(context.Context, Request, Result) (Result, error)
	CreateRemoteRef(context.Context, Request, Result) (Result, error)
	Reconcile(context.Context, Request, Result) (Result, error)
}

type PreflightRequest struct {
	Workspace                  fix.WorkspaceIdentity
	Plan                       fix.DeliveryPlan
	Remote, BaseBranch, Branch string
	Publication                bool
	CommandOutputBytes         int64
}

// PreflightResult is the provider identity derived from the configured remote
// by the Git boundary. Callers must not reconstruct it from preferences or a
// mutable candidate after preflight.
type PreflightResult struct {
	RemoteHost     string
	HostRepository string
	// RemoteIdentity is an opaque credential-free fingerprint of the exact
	// normalized push URL admitted before any repository mutation.
	RemoteIdentity string
}

type PreflightService interface {
	Preflight(context.Context, PreflightRequest) (PreflightResult, error)
}
