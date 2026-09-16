package fixapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/publisher"
)

func (owner *publicationOwner) preflightDelivery(ctx context.Context, workspace fix.WorkspaceIdentity, plan fix.DeliveryPlan, config appconfig.Delivery, branch string, publication bool) (delivery.PreflightResult, error) {
	if !plan.Valid() {
		return delivery.PreflightResult{}, fmt.Errorf("unsupported delivery plan %+v", plan)
	}
	if plan.Git == fix.GitLeaveUncommitted {
		return delivery.PreflightResult{}, nil
	}
	if err := validatePreflightPlan(plan, config, branch); err != nil {
		return delivery.PreflightResult{}, err
	}
	preflight := owner.deliveryPreflight()
	if preflight == nil {
		return delivery.PreflightResult{}, errors.New("delivery preflight service is unavailable")
	}
	target, err := preflight.Preflight(ctx, delivery.PreflightRequest{Workspace: workspace, Plan: plan, Remote: config.Remote,
		BaseBranch: config.BaseBranch, Branch: branch, Publication: publication, CommandOutputBytes: config.CommandOutputBytes})
	if err != nil {
		return delivery.PreflightResult{}, fmt.Errorf("delivery preflight: %w", err)
	}
	if err := owner.preflightPullRequest(ctx, workspace, plan, config, target); err != nil {
		return delivery.PreflightResult{}, err
	}
	return target, nil
}

func validatePreflightPlan(plan fix.DeliveryPlan, config appconfig.Delivery, branch string) error {
	if plan.Git == fix.GitCommitNewBranch && branch == "" {
		return errors.New("new-branch delivery requires a branch name")
	}
	if plan.Publish != fix.PublishLocal && config.Remote == "" {
		return errors.New("pushing requires a configured remote")
	}
	if plan.Publish == fix.PublishPullRequest && config.BaseBranch == "" {
		return errors.New("pull-request delivery requires an explicit base branch")
	}
	return nil
}

func (owner *publicationOwner) deliveryPreflight() delivery.PreflightService {
	if owner.preflight != nil {
		return owner.preflight
	}
	preflight, _ := owner.delivery.(delivery.PreflightService)
	return preflight
}

func (owner *publicationOwner) preflightPullRequest(ctx context.Context, workspace fix.WorkspaceIdentity, plan fix.DeliveryPlan, config appconfig.Delivery, target delivery.PreflightResult) error {
	if plan.Publish != fix.PublishPullRequest {
		return nil
	}
	if config.Publisher != "github-cli" {
		return fmt.Errorf("pull-request publisher %q is unsupported", config.Publisher)
	}
	if !strings.EqualFold(target.RemoteHost, "github.com") || target.HostRepository == "" {
		return errors.New("pull-request delivery requires a canonical github.com owner/repository remote")
	}
	publisherPreflight, ok := owner.publisher.(publisher.PreflightService)
	if !ok {
		return errors.New("pull-request publisher preflight is unavailable")
	}
	_, err := publisherPreflight.Preflight(ctx, publisher.PreflightRequest{Provider: config.Publisher, RepositoryRoot: workspace.RepositoryRoot,
		RemoteHost: target.RemoteHost, HostRepository: target.HostRepository, Draft: config.DraftPullRequests, CommandOutputBytes: config.CommandOutputBytes})
	if err != nil {
		return fmt.Errorf("publisher preflight: %w", err)
	}
	return nil
}
