package candidate

import (
	"context"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/fix"
)

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
	preparation := newCandidatePreparation(service.executor, &service.registry, service.stateRoot, request, discovered, ownership)
	return preparation.run(ctx)
}
