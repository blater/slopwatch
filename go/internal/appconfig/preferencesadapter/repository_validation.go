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
		candidate, err := preferenceFixToApp(*value.Fix)
		if err != nil {
			return err
		}
		if candidate.TargetScore > inherited.Fix.TargetScore {
			return repositoryBroadening("fix target score", candidate.TargetScore, inherited.Fix.TargetScore)
		}
		if !metricSuperset(candidate.Focus, inherited.Fix.Focus) {
			return fmt.Errorf("repository preferences cannot remove inherited fix focus metrics")
		}
		if mutationScopeRank(candidate.ChangeScope) > mutationScopeRank(inherited.Fix.ChangeScope) {
			return repositoryBroadening("fix change scope", candidate.ChangeScope, inherited.Fix.ChangeScope)
		}
		if candidate.Profile != inherited.Fix.Profile {
			return repositoryOwnedField("fix profile")
		}
		if candidate.Model != inherited.Fix.Model {
			return repositoryOwnedField("fix model")
		}
		if candidate.Effort != inherited.Fix.Effort {
			return repositoryOwnedField("fix effort")
		}
		if candidate.PromptTemplate != inherited.Fix.PromptTemplate {
			return repositoryOwnedField("fix prompt template")
		}
	}
	if value.Concurrency != nil {
		candidate := preferenceConcurrencyToApp(*value.Concurrency)
		if candidate.MaxAgents > inherited.Concurrency.MaxAgents {
			return repositoryBroadening("concurrency max agents", candidate.MaxAgents, inherited.Concurrency.MaxAgents)
		}
		if candidate.MaxVerifiers > inherited.Concurrency.MaxVerifiers {
			return repositoryBroadening("concurrency max verifiers", candidate.MaxVerifiers, inherited.Concurrency.MaxVerifiers)
		}
		if candidate.MaxActorsPerJob > inherited.Concurrency.MaxActorsPerJob {
			return repositoryBroadening("concurrency actors per job", candidate.MaxActorsPerJob, inherited.Concurrency.MaxActorsPerJob)
		}
		if candidate.MaxCandidatePreviewBytes > inherited.Concurrency.MaxCandidatePreviewBytes {
			return repositoryBroadening("candidate preview bytes", candidate.MaxCandidatePreviewBytes, inherited.Concurrency.MaxCandidatePreviewBytes)
		}
		if candidate.MaxCandidatePreviewLines > inherited.Concurrency.MaxCandidatePreviewLines {
			return repositoryBroadening("candidate preview lines", candidate.MaxCandidatePreviewLines, inherited.Concurrency.MaxCandidatePreviewLines)
		}
	}
	if value.Delivery != nil {
		candidate := preferenceDeliveryToApp(*value.Delivery)
		if workspaceModeRank(candidate.DefaultPlan.Workspace) > workspaceModeRank(inherited.Delivery.DefaultPlan.Workspace) ||
			gitModeRank(candidate.DefaultPlan.Git) > gitModeRank(inherited.Delivery.DefaultPlan.Git) ||
			publishModeRank(candidate.DefaultPlan.Publish) > publishModeRank(inherited.Delivery.DefaultPlan.Publish) {
			return repositoryBroadening("delivery plan", candidate.DefaultPlan, inherited.Delivery.DefaultPlan)
		}
		if candidate.Remote != inherited.Delivery.Remote {
			return repositoryOwnedField("delivery remote")
		}
		if candidate.BaseBranch != inherited.Delivery.BaseBranch {
			return repositoryOwnedField("delivery base branch")
		}
		if candidate.BranchTemplate != inherited.Delivery.BranchTemplate {
			return repositoryOwnedField("delivery branch template")
		}
		if candidate.Publisher != inherited.Delivery.Publisher {
			return repositoryOwnedField("delivery publisher")
		}
		if inherited.Delivery.DraftPullRequests && !candidate.DraftPullRequests {
			return repositoryBroadening("draft pull request policy", candidate.DraftPullRequests, inherited.Delivery.DraftPullRequests)
		}
		if candidate.CommandOutputBytes > inherited.Delivery.CommandOutputBytes {
			return repositoryBroadening("delivery command output bytes", candidate.CommandOutputBytes, inherited.Delivery.CommandOutputBytes)
		}
		if candidate.CommitTitleTemplate != inherited.Delivery.CommitTitleTemplate {
			return repositoryOwnedField("delivery commit title template")
		}
		if candidate.CommitBodyTemplate != inherited.Delivery.CommitBodyTemplate {
			return repositoryOwnedField("delivery commit body template")
		}
		if candidate.PullRequestTitleTemplate != inherited.Delivery.PullRequestTitleTemplate {
			return repositoryOwnedField("delivery pull request title template")
		}
		if candidate.PullRequestBodyTemplate != inherited.Delivery.PullRequestBodyTemplate {
			return repositoryOwnedField("delivery pull request body template")
		}
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
