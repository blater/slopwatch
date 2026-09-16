package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func validatePreflightRequest(request PreflightRequest) error {
	if !request.Plan.Valid() {
		return errors.New("delivery plan is invalid")
	}
	if request.Plan.Git == fix.GitLeaveUncommitted {
		return nil
	}
	if request.Workspace.RepositoryRoot == "" {
		return errors.New("delivery repository is required")
	}
	if request.Plan.Publish != fix.PublishLocal && !validRemoteAlias(request.Remote) {
		return errors.New("delivery remote must be a safe configured remote alias")
	}
	return nil
}

func validateNewBranch(executor gitExecutor, ctx context.Context, request PreflightRequest) error {
	if request.Plan.Git != fix.GitCommitNewBranch {
		return nil
	}
	if request.Branch == "" {
		return errors.New("new-branch delivery requires a branch name")
	}
	if err := validateLiteralBranch(executor, ctx, request.Workspace.RepositoryRoot, request.Branch); err != nil {
		return fmt.Errorf("invalid delivery branch: %w", err)
	}
	return nil
}

func validateLocalBranchAvailable(executor gitExecutor, ctx context.Context, request PreflightRequest) error {
	if request.Plan.Git != fix.GitCommitNewBranch {
		return nil
	}
	ref := "refs/heads/" + request.Branch
	exists, _, err := executor.ref(ctx, request.Workspace.RepositoryRoot, ref)
	if err != nil {
		return fmt.Errorf("check local delivery branch: %w", err)
	}
	if exists {
		return errors.New("delivery branch already exists locally")
	}
	return nil
}

func admitRemote(executor gitExecutor, ctx context.Context, request PreflightRequest) (PreflightResult, string, error) {
	remoteURL, err := executor.resolveRemoteURL(ctx, request.Workspace.RepositoryRoot, request.Remote)
	if err != nil {
		return PreflightResult{}, "", fmt.Errorf("delivery remote %q is unavailable: %w", request.Remote, err)
	}
	target, err := remoteProviderTarget(remoteURL)
	if err != nil {
		return PreflightResult{}, "", fmt.Errorf("delivery remote %q has no supported provider identity: %w", request.Remote, err)
	}
	target.RemoteIdentity, err = remoteIdentity(remoteURL)
	if err != nil {
		return PreflightResult{}, "", fmt.Errorf("delivery remote %q identity is invalid: %w", request.Remote, err)
	}
	if request.Plan.Publish == fix.PublishPullRequest && (!strings.EqualFold(target.RemoteHost, "github.com") || target.HostRepository == "") {
		return PreflightResult{}, "", errors.New("pull-request delivery requires a canonical github.com owner/repository remote")
	}
	return target, remoteURL, nil
}

func validateRemoteBranch(executor gitExecutor, ctx context.Context, request PreflightRequest, remoteURL string) error {
	if request.Plan.Git != fix.GitCommitNewBranch {
		return nil
	}
	if err := validateLocalBranchAvailable(executor, ctx, request); err != nil {
		return err
	}
	ref := "refs/heads/" + request.Branch
	exists, _, err := executor.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, ref)
	if err != nil {
		return fmt.Errorf("check remote delivery branch: %w", err)
	}
	if exists {
		return errors.New("delivery branch already exists remotely")
	}
	return nil
}

func validatePullRequestBase(executor gitExecutor, ctx context.Context, request PreflightRequest, remoteURL string) error {
	if request.Plan.Publish != fix.PublishPullRequest || !request.Publication {
		return nil
	}
	if request.BaseBranch == "" {
		return errors.New("pull-request delivery requires an explicit base branch")
	}
	if err := validateLiteralBranch(executor, ctx, request.Workspace.RepositoryRoot, request.BaseBranch); err != nil {
		return fmt.Errorf("pull-request base branch %q is not a literal branch: %w", request.BaseBranch, err)
	}
	baseRef := "refs/heads/" + request.BaseBranch
	exists, _, err := executor.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, baseRef)
	if err != nil {
		return fmt.Errorf("check pull-request base branch %q on admitted remote: %w", request.BaseBranch, err)
	}
	if !exists {
		return fmt.Errorf("pull-request base branch %q does not exist on the admitted remote", request.BaseBranch)
	}
	return nil
}
