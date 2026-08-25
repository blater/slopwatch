// Package publisher defines provider-specific pull-request publication after
// delivery has created and verified an exact remote ref.
package publisher

import (
	"context"

	"github.com/blater/slopwatch/internal/fix"
)

type Request struct {
	Job        fix.JobID
	Repository fix.RepositoryID
	Candidate  fix.CandidateIdentity
	// HostRepository is the verified provider identity (for GitHub,
	// "owner/name") derived from the delivered remote, never inferred by the
	// publisher from mutable candidate configuration.
	HostRepository string
	Remote         string
	BaseBranch     string
	HeadBranch     string
	Commit         fix.ObjectID
	Title          string
	Body           string
	Draft          bool
}

type Result struct {
	ProviderID string
	URL        string
	Draft      bool
	Ambiguous  bool
	Diagnostic string
}

type Service interface {
	Create(context.Context, Request) (Result, error)
	Reconcile(context.Context, Request, Result) (Result, error)
}
