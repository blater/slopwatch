package fixapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/fixprompt"
)

func (manager *Manager) LoadFix(ctx context.Context, request LoadRequest) (FixInput, error) {
	if manager.controller.closed.Load() {
		return FixInput{}, ErrClosed
	}
	if len(request.Targets) == 0 {
		return FixInput{}, errors.New("load fix: at least one target is required")
	}
	resolved, err := manager.controller.deps.Config.Resolve(ctx, request.Workspace, request.Overrides)
	if err != nil {
		return FixInput{}, fmt.Errorf("load fix preferences: %w", err)
	}
	profile, err := selectedProfile(resolved)
	if err != nil {
		return FixInput{}, err
	}
	baseline, err := manager.loadBaseline(ctx, request, resolved)
	if err != nil {
		return FixInput{}, err
	}
	plannedPaths, allowedPaths, err := manager.planPaths(ctx, request, resolved)
	if err != nil {
		return FixInput{}, err
	}
	probe := manager.controller.deps.Agents.Probe(ctx, profile)
	model, effort := resolveAgentOptions(probe, resolved)
	branchName, deliveryPlan, err := manager.resolveDelivery(request, resolved)
	if err != nil {
		return FixInput{}, err
	}
	baseline.Contract.Goal.Focus, err = configuredFocusGoals(resolved.Fix.Focus, baseline.Contract.Targets, resolved.Fix.TargetScore)
	if err != nil {
		return FixInput{}, err
	}
	instructions, err := fixprompt.Compile(fixprompt.Input{Contract: baseline.Contract, AllowedScope: resolved.Fix.ChangeScope,
		AllowedPaths: allowedPaths, BranchName: branchName, Template: resolved.Fix.PromptTemplate})
	if err != nil {
		return FixInput{}, err
	}
	return FixInput{Workspace: request.Workspace, Targets: append([]fix.RepoPath(nil), request.Targets...), Baseline: baseline,
		Preferences: resolved, Profile: cloneProfile(profile), Probe: cloneProbe(probe), Model: model, Effort: effort,
		TargetScore: resolved.Fix.TargetScore, Focus: append([]fix.MetricGoal(nil), baseline.Contract.Goal.Focus...),
		ChangeScope: resolved.Fix.ChangeScope, AllowedPaths: append([]fix.RepoPath(nil), allowedPaths...),
		DeliveryPlan: deliveryPlan, BranchName: branchName, Instructions: instructions,
		PlannedPaths: append([]fix.RepoPath(nil), plannedPaths...)}, nil
}

func (manager *Manager) loadBaseline(ctx context.Context, request LoadRequest, resolved appconfig.Resolved) (fixanalysis.BaselineSnapshot, error) {
	baseline, err := manager.controller.deps.Analysis.PrepareBaseline(ctx, fixanalysis.BaselineRequest{
		Workspace: request.Workspace, Targets: append([]fix.RepoPath(nil), request.Targets...),
		Goal: fix.ScoringGoal{MaximumScore: resolved.Fix.TargetScore}, RequiredMetrics: measuredFocusMetrics(resolved.Fix.Focus),
	})
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("load fix baseline: %w", err)
	}
	return baseline, nil
}

func (manager *Manager) planPaths(ctx context.Context, request LoadRequest, resolved appconfig.Resolved) ([]fix.RepoPath, []fix.RepoPath, error) {
	planned := append([]fix.RepoPath(nil), request.Targets...)
	if manager.controller.deps.ScopePlanner != nil {
		var err error
		planned, err = manager.controller.deps.ScopePlanner.Plan(ctx, request.Workspace, request.Targets, "targets-and-tests")
		if err != nil {
			return nil, nil, fmt.Errorf("load fix change scope: %w", err)
		}
	}
	allowed, err := pathsForScope(resolved.Fix.ChangeScope, request.Targets, planned)
	if err != nil {
		return nil, nil, err
	}
	return planned, allowed, nil
}

func pathsForScope(scope string, targets, planned []fix.RepoPath) ([]fix.RepoPath, error) {
	switch scope {
	case "targets", "targets-only":
		return append([]fix.RepoPath(nil), targets...), nil
	case "targets-and-tests":
		return append([]fix.RepoPath(nil), planned...), nil
	case "repository":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported change scope %q", scope)
	}
}

func resolveAgentOptions(probe agent.ProbeResult, resolved appconfig.Resolved) (agent.ModelID, agent.EffortID) {
	model, effort := resolved.Fix.Model, resolved.Fix.Effort
	if selected, ok := agent.ResolveOption(probe.Capabilities.Models, model); ok {
		model = selected
	}
	if selected, ok := agent.ResolveOption(probe.Capabilities.Efforts, effort); ok {
		effort = selected
	}
	return model, effort
}

func (manager *Manager) resolveDelivery(request LoadRequest, resolved appconfig.Resolved) (string, fix.DeliveryPlan, error) {
	seed, err := fix.NewJobID()
	if err != nil {
		return "", fix.DeliveryPlan{}, err
	}
	branch := renderBranch(effectiveBranchTemplate(resolved), request.Targets, string(seed), resolved.Fix.Focus, manager.controller.options.Clock())
	plan := resolved.Delivery.DefaultPlan
	if request.Delivery != nil {
		plan, branch = request.Delivery.Plan, request.Delivery.Branch
	}
	if request.Workspace.GitCommonDir == "" {
		plan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitLeaveUncommitted, Publish: fix.PublishLocal}
	}
	if plan.Workspace == fix.WorkspaceWorktree && plan.Git == fix.GitCommitCurrent {
		plan.Git, plan.Publish = fix.GitLeaveUncommitted, fix.PublishLocal
	}
	if plan.Git == fix.GitCommitCurrent {
		branch = request.Workspace.CurrentBranch
	} else if plan.Git == fix.GitLeaveUncommitted {
		branch = ""
	}
	return branch, plan, nil
}

func effectiveBranchTemplate(resolved appconfig.Resolved) string {
	return strings.TrimSpace(resolved.Delivery.BranchTemplate)
}

func selectedProfile(resolved appconfig.Resolved) (agent.Profile, error) {
	for _, profile := range resolved.Profiles {
		if profile.ID == resolved.Fix.Profile {
			return cloneProfile(profile), nil
		}
	}
	return agent.Profile{}, fmt.Errorf("load fix: agent profile %q is not configured", resolved.Fix.Profile)
}

func renderBranch(template string, targets []fix.RepoPath, seed string, focus []fix.MetricID, now time.Time) string {
	if template == "" {
		template = "slopwatch/fix/{target-stem}-{job-short-id}"
	}
	target := targetStem(targets)
	metrics := make([]string, 0, len(focus))
	for _, metric := range focus {
		metrics = append(metrics, sanitizeBranchPart(string(metric)))
	}
	return strings.NewReplacer("{target-stem}", sanitizeBranchPart(target), "{job-short-id}", strings.TrimPrefix(seed, "job-"),
		"{date}", now.UTC().Format("20060102"), "{metrics}", sanitizeBranchPart(strings.Join(metrics, "-"))).Replace(template)
}

func targetStem(targets []fix.RepoPath) string {
	if len(targets) == 0 {
		return "target"
	}
	target := targets[0].String()
	if slash := strings.LastIndex(target, "/"); slash >= 0 {
		target = target[slash+1:]
	}
	if dot := strings.LastIndex(target, "."); dot > 0 {
		target = target[:dot]
	}
	return target
}

func sanitizeBranchPart(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		if allowedBranchRune(character) {
			result.WriteRune(character)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-_")
}

func allowedBranchRune(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_'
}
