// Package delivery owns local commit/ref creation and exact remote-ref push.
// Agent runtimes never receive this service.
package delivery

import (
	"context"

	"github.com/blater/slopwatch/internal/fix"
)

type Request struct {
	Job         fix.JobID
	Candidate   fix.CandidateIdentity
	DiffHash    string
	Branch      string
	Remote      string
	CommitTitle string
	CommitBody  string
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
// used by the fix controller. Each successful return can be journaled before
// the next externally visible side effect begins.
type SagaService interface {
	CreateCommit(context.Context, Request) (Result, error)
	CreateLocalRef(context.Context, Request, Result) (Result, error)
	CreateRemoteRef(context.Context, Request, Result) (Result, error)
	Reconcile(context.Context, Request, Result) (Result, error)
}

type PreflightRequest struct {
	Workspace                  fix.WorkspaceIdentity
	Mode                       fix.DeliveryMode
	Remote, BaseBranch, Branch string
}
type PreflightService interface {
	Preflight(context.Context, PreflightRequest) error
}
