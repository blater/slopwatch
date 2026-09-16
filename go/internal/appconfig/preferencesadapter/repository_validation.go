package preferencesadapter

import (
	"fmt"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
)

func validateRepositoryPartial(value preferences.PartialDocument) error {
	if value.Agents != nil && len(value.Agents.Profiles) != 0 {
		return fmt.Errorf("repository preferences cannot register agent profiles, commands, or credentials")
	}
	return nil
}

// validateRepositoryOverride is the repository allowlist. The checked-out
// repository may tighten goals, paths, delivery and bounded resource limits,
// but it cannot choose identities, templates or destinations owned by the
// user. Fields without a provider-independent ordering must remain unchanged.
func validateRepositoryOverride(inherited appconfig.Resolved, value preferences.PartialDocument) error {
	if value.Fix != nil {
		if err := validateRepositoryFix(inherited.Fix, *value.Fix); err != nil {
			return err
		}
	}
	if value.Concurrency != nil {
		if err := validateRepositoryConcurrency(inherited.Concurrency, *value.Concurrency); err != nil {
			return err
		}
	}
	if value.Delivery != nil {
		if err := validateRepositoryDelivery(inherited.Delivery, *value.Delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateRepositoryFix(inherited appconfig.FixDefaults, value preferences.Fix) error {
	candidate, err := preferenceFixToApp(value)
	if err != nil {
		return err
	}
	if candidate.TargetScore > inherited.TargetScore {
		return repositoryBroadening("fix target score", candidate.TargetScore, inherited.TargetScore)
	}
	if !metricSuperset(candidate.Focus, inherited.Focus) {
		return fmt.Errorf("repository preferences cannot remove inherited fix focus metrics")
	}
	if mutationScopeRank(candidate.ChangeScope) > mutationScopeRank(inherited.ChangeScope) {
		return repositoryBroadening("fix change scope", candidate.ChangeScope, inherited.ChangeScope)
	}
	for _, field := range []struct{ name, candidate, inherited string }{
		{"fix profile", string(candidate.Profile), string(inherited.Profile)},
		{"fix model", string(candidate.Model), string(inherited.Model)},
		{"fix effort", string(candidate.Effort), string(inherited.Effort)},
		{"fix prompt template", candidate.PromptTemplate, inherited.PromptTemplate},
	} {
		if field.candidate != field.inherited {
			return repositoryOwnedField(field.name)
		}
	}
	return nil
}

func validateRepositoryConcurrency(inherited appconfig.Concurrency, value preferences.Concurrency) error {
	candidate := preferenceConcurrencyToApp(value)
	for _, field := range []struct {
		name      string
		candidate int64
		inherited int64
	}{
		{"concurrency max agents", int64(candidate.MaxAgents), int64(inherited.MaxAgents)},
		{"concurrency max verifiers", int64(candidate.MaxVerifiers), int64(inherited.MaxVerifiers)},
		{"concurrency actors per job", int64(candidate.MaxActorsPerJob), int64(inherited.MaxActorsPerJob)},
		{"candidate preview bytes", candidate.MaxCandidatePreviewBytes, inherited.MaxCandidatePreviewBytes},
		{"candidate preview lines", int64(candidate.MaxCandidatePreviewLines), int64(inherited.MaxCandidatePreviewLines)},
	} {
		if field.candidate > field.inherited {
			return repositoryBroadening(field.name, field.candidate, field.inherited)
		}
	}
	return nil
}

func validateRepositoryDelivery(inherited appconfig.Delivery, value preferences.Delivery) error {
	candidate := preferenceDeliveryToApp(value)
	if err := validateRepositoryDeliveryPlan(inherited.DefaultPlan, candidate.DefaultPlan); err != nil {
		return err
	}
	for _, field := range []struct{ name, candidate, inherited string }{
		{"delivery remote", candidate.Remote, inherited.Remote},
		{"delivery base branch", candidate.BaseBranch, inherited.BaseBranch},
		{"delivery branch template", candidate.BranchTemplate, inherited.BranchTemplate},
		{"delivery publisher", candidate.Publisher, inherited.Publisher},
	} {
		if field.candidate != field.inherited {
			return repositoryOwnedField(field.name)
		}
	}
	if inherited.DraftPullRequests && !candidate.DraftPullRequests {
		return repositoryBroadening("draft pull request policy", candidate.DraftPullRequests, inherited.DraftPullRequests)
	}
	if candidate.CommandOutputBytes > inherited.CommandOutputBytes {
		return repositoryBroadening("delivery command output bytes", candidate.CommandOutputBytes, inherited.CommandOutputBytes)
	}
	for _, field := range []struct{ name, candidate, inherited string }{
		{"delivery commit title template", candidate.CommitTitleTemplate, inherited.CommitTitleTemplate},
		{"delivery commit body template", candidate.CommitBodyTemplate, inherited.CommitBodyTemplate},
		{"delivery pull request title template", candidate.PullRequestTitleTemplate, inherited.PullRequestTitleTemplate},
		{"delivery pull request body template", candidate.PullRequestBodyTemplate, inherited.PullRequestBodyTemplate},
	} {
		if field.candidate != field.inherited {
			return repositoryOwnedField(field.name)
		}
	}
	return nil
}

func validateRepositoryDeliveryPlan(inherited, candidate fix.DeliveryPlan) error {
	if workspaceModeRank(candidate.Workspace) > workspaceModeRank(inherited.Workspace) {
		return repositoryBroadening("delivery plan", candidate, inherited)
	}
	if gitModeRank(candidate.Git) > gitModeRank(inherited.Git) {
		return repositoryBroadening("delivery plan", candidate, inherited)
	}
	if publishModeRank(candidate.Publish) > publishModeRank(inherited.Publish) {
		return repositoryBroadening("delivery plan", candidate, inherited)
	}
	return nil
}

func repositoryBroadening(field string, candidate, inherited any) error {
	return fmt.Errorf("repository preferences cannot broaden %s: %v exceeds inherited %v", field, candidate, inherited)
}

func repositoryOwnedField(field string) error {
	return fmt.Errorf("repository preferences cannot override user-owned %s", field)
}

func metricSuperset(candidate, inherited []fix.MetricID) bool {
	available := make(map[fix.MetricID]struct{}, len(candidate))
	for _, metric := range candidate {
		available[metric] = struct{}{}
	}
	for _, metric := range inherited {
		if _, exists := available[metric]; !exists {
			return false
		}
	}
	return true
}

func mutationScopeRank(value string) int {
	switch value {
	case "targets-only":
		return 0
	case "targets-and-tests":
		return 1
	case "repository":
		return 2
	default:
		// Normal configuration checks report the unsupported value. Treat it
		// as broad here so it can never slip through a repository comparison.
		return 3
	}
}

func workspaceModeRank(value fix.WorkspaceMode) int {
	switch value {
	case fix.WorkspaceWorktree:
		return 0
	case fix.WorkspaceCurrent:
		return 1
	default:
		return 2
	}
}

func gitModeRank(value fix.GitMode) int {
	switch value {
	case fix.GitLeaveUncommitted:
		return 0
	case fix.GitCommitNewBranch:
		return 1
	case fix.GitCommitCurrent:
		return 2
	default:
		return 3
	}
}

func publishModeRank(value fix.PublishMode) int {
	switch value {
	case fix.PublishLocal:
		return 0
	case fix.PublishPush:
		return 1
	case fix.PublishPullRequest:
		return 2
	default:
		return 3
	}
}
