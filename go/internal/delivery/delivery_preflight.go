package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *GitService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if !request.Plan.Valid() {
		return PreflightResult{}, errors.New("delivery plan is invalid")
	}
	if request.Plan.Git == fix.GitLeaveUncommitted {
		return PreflightResult{}, nil
	}
	if request.Workspace.RepositoryRoot == "" {
		return PreflightResult{}, errors.New("delivery repository is required")
	}
	if request.Plan.Publish != fix.PublishLocal && !validRemoteAlias(request.Remote) {
		return PreflightResult{}, errors.New("delivery remote must be a safe configured remote alias")
	}
	if request.Plan.Git == fix.GitCommitNewBranch {
		if request.Branch == "" {
			return PreflightResult{}, errors.New("new-branch delivery requires a branch name")
		}
		if err := service.validateLiteralBranch(ctx, request.Workspace.RepositoryRoot, request.Branch); err != nil {
			return PreflightResult{}, fmt.Errorf("invalid delivery branch: %w", err)
		}
	}
	if request.Plan.Publish == fix.PublishLocal {
		if request.Plan.Git == fix.GitCommitNewBranch {
			ref := "refs/heads/" + request.Branch
			if exists, _, err := service.ref(ctx, request.Workspace.RepositoryRoot, ref); err != nil {
				return PreflightResult{}, fmt.Errorf("check local delivery branch: %w", err)
			} else if exists {
				return PreflightResult{}, errors.New("delivery branch already exists locally")
			}
		}
		return PreflightResult{}, nil
	}
	remoteURL, err := service.resolveRemoteURL(ctx, request.Workspace.RepositoryRoot, request.Remote)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q is unavailable: %w", request.Remote, err)
	}
	target, err := remoteProviderTarget(remoteURL)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q has no supported provider identity: %w", request.Remote, err)
	}
	target.RemoteIdentity, err = remoteIdentity(remoteURL)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("delivery remote %q identity is invalid: %w", request.Remote, err)
	}
	if request.Plan.Publish == fix.PublishPullRequest && (!strings.EqualFold(target.RemoteHost, "github.com") || target.HostRepository == "") {
		return PreflightResult{}, errors.New("pull-request delivery requires a canonical github.com owner/repository remote")
	}
	if request.Plan.Git == fix.GitCommitNewBranch {
		ref := "refs/heads/" + request.Branch
		if exists, _, err := service.ref(ctx, request.Workspace.RepositoryRoot, ref); err != nil {
			return PreflightResult{}, fmt.Errorf("check local delivery branch: %w", err)
		} else if exists {
			return PreflightResult{}, errors.New("delivery branch already exists locally")
		}
		if exists, _, err := service.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, ref); err != nil {
			return PreflightResult{}, fmt.Errorf("check remote delivery branch: %w", err)
		} else if exists {
			return PreflightResult{}, errors.New("delivery branch already exists remotely")
		}
	}
	if request.Plan.Publish == fix.PublishPullRequest && request.Publication {
		if request.BaseBranch == "" {
			return PreflightResult{}, errors.New("pull-request delivery requires an explicit base branch")
		}
		if err := service.validateLiteralBranch(ctx, request.Workspace.RepositoryRoot, request.BaseBranch); err != nil {
			return PreflightResult{}, fmt.Errorf("pull-request base branch %q is not a literal branch: %w", request.BaseBranch, err)
		}
		baseRef := "refs/heads/" + request.BaseBranch
		if exists, _, err := service.remoteRefURL(ctx, request.Workspace.RepositoryRoot, remoteURL, baseRef); err != nil {
			return PreflightResult{}, fmt.Errorf("check pull-request base branch %q on admitted remote: %w", request.BaseBranch, err)
		} else if !exists {
			return PreflightResult{}, fmt.Errorf("pull-request base branch %q does not exist on the admitted remote", request.BaseBranch)
		}
	}
	return target, nil
}

func (service *GitService) validateLiteralBranch(ctx context.Context, root, branch string) error {
	data, err := service.executor.bytes(ctx, root, false, "check-ref-format", "--branch", branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != branch {
		return errors.New("Git resolved the value as branch shorthand")
	}
	return nil
}
