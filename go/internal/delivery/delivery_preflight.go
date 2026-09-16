package delivery

import (
	"context"
	"errors"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if err := validatePreflightRequest(request); err != nil {
		return PreflightResult{}, err
	}
	if request.Plan.Git == fix.GitLeaveUncommitted {
		return PreflightResult{}, nil
	}
	if err := validateNewBranch(service.executor, ctx, request); err != nil {
		return PreflightResult{}, err
	}
	if request.Plan.Publish == fix.PublishLocal {
		return PreflightResult{}, validateLocalBranchAvailable(service.executor, ctx, request)
	}
	target, remoteURL, err := admitRemote(service.executor, ctx, request)
	if err != nil {
		return PreflightResult{}, err
	}
	if err := validateRemoteBranch(service.executor, ctx, request, remoteURL); err != nil {
		return PreflightResult{}, err
	}
	if err := validatePullRequestBase(service.executor, ctx, request, remoteURL); err != nil {
		return PreflightResult{}, err
	}
	return target, nil
}

func validateLiteralBranch(executor gitExecutor, ctx context.Context, root, branch string) error {
	data, err := executor.bytes(ctx, root, false, "check-ref-format", "--branch", branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != branch {
		return errors.New("Git resolved the value as branch shorthand")
	}
	return nil
}
