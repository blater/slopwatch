package preferencesadapter

import (
	"reflect"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/preferences"
)

func builtInOrigins() map[string]appconfig.Origin {
	result := map[string]appconfig.Origin{
		"fix":                      appconfig.OriginBuiltIn,
		"concurrency":              appconfig.OriginBuiltIn,
		"agents":                   appconfig.OriginBuiltIn,
		"delivery":                 appconfig.OriginBuiltIn,
		"interaction.trend_window": appconfig.OriginBuiltIn,
	}
	for _, key := range []string{"fix.target_score", "fix.focus", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.prompt_template", "concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_actors_per_job", "concurrency.max_candidate_preview_bytes", "concurrency.max_candidate_preview_lines", "delivery.workspace", "delivery.git", "delivery.publish", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests", "delivery.command_output_bytes"} {
		result[key] = appconfig.OriginBuiltIn
	}
	return result
}

func markUserOrigins(resolved *appconfig.Resolved, partial preferences.PartialDocument, defaults, user preferences.Document) {
	markUserFixOrigins(resolved.Origins, partial.Fix, defaults.Fix, user.Fix)
	markUserConcurrencyOrigins(resolved.Origins, partial.Concurrency, defaults.Concurrency, user.Concurrency)
	markUserAgentOrigins(resolved.Origins, partial.Agents, defaults.Agents, user.Agents)
	markUserDeliveryOrigins(resolved.Origins, partial.Delivery, defaults.Delivery, user.Delivery)
	if partial.Interaction != nil && defaults.Interaction.TrendWindow != user.Interaction.TrendWindow {
		resolved.Origins["interaction.trend_window"] = appconfig.OriginUser
	}
}

func markUserFixOrigins(origins map[string]appconfig.Origin, partial *preferences.Fix, defaults, user preferences.Fix) {
	if partial != nil && !reflect.DeepEqual(defaults, user) {
		origins["fix"] = appconfig.OriginUser
		markFixFieldOrigins(origins, defaults, user, appconfig.OriginUser)
		markFixListOrigins(origins, defaults, user, appconfig.OriginUser)
	}
}

func markUserConcurrencyOrigins(origins map[string]appconfig.Origin, partial *preferences.Concurrency, defaults, user preferences.Concurrency) {
	if partial != nil && !reflect.DeepEqual(defaults, user) {
		origins["concurrency"] = appconfig.OriginUser
		markConcurrencyFieldOrigins(origins, defaults, user, appconfig.OriginUser)
	}
}

func markUserAgentOrigins(origins map[string]appconfig.Origin, partial *preferences.Agents, defaults, user preferences.Agents) {
	if partial != nil && !reflect.DeepEqual(defaults, user) {
		origins["agents"] = appconfig.OriginUser
		markProfileEntryOrigins(origins, defaults.Profiles, user.Profiles, appconfig.OriginUser)
	}
}

func markUserDeliveryOrigins(origins map[string]appconfig.Origin, partial *preferences.Delivery, defaults, user preferences.Delivery) {
	if partial != nil && !reflect.DeepEqual(defaults, user) {
		origins["delivery"] = appconfig.OriginUser
		markDeliveryFieldOrigins(origins, defaults, user, appconfig.OriginUser)
	}
}

func markFixFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Fix, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"fix.target_score", before.TargetScore != after.TargetScore}, {"fix.focus", !reflect.DeepEqual(before.Focus, after.Focus)}, {"fix.change_scope", before.ChangeScope != after.ChangeScope}, {"fix.profile", before.Profile != after.Profile}, {"fix.model", before.Model != after.Model}, {"fix.effort", before.Effort != after.Effort}, {"fix.prompt_template", before.PromptTemplate != after.PromptTemplate}}
	for _, value := range values {
		if value.changed {
			origins[value.key] = origin
		}
	}
}
func markConcurrencyFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Concurrency, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"concurrency.max_agents", before.MaxAgents != after.MaxAgents}, {"concurrency.max_verifiers", before.MaxVerifiers != after.MaxVerifiers}, {"concurrency.max_actors_per_job", before.MaxActorsPerJob != after.MaxActorsPerJob}, {"concurrency.max_candidate_preview_bytes", before.MaxCandidatePreviewBytes != after.MaxCandidatePreviewBytes}, {"concurrency.max_candidate_preview_lines", before.MaxCandidatePreviewLines != after.MaxCandidatePreviewLines}}
	for _, v := range values {
		if v.changed {
			origins[v.key] = origin
		}
	}
}
func markDeliveryFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Delivery, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"delivery.workspace", before.Workspace != after.Workspace}, {"delivery.git", before.Git != after.Git}, {"delivery.publish", before.Publish != after.Publish}, {"delivery.remote", before.Remote != after.Remote}, {"delivery.base_branch", before.BaseBranch != after.BaseBranch}, {"delivery.branch_template", before.BranchTemplate != after.BranchTemplate}, {"delivery.publisher", before.Publisher != after.Publisher}, {"delivery.draft_pull_requests", before.DraftPullRequests != after.DraftPullRequests}, {"delivery.command_output_bytes", before.CommandOutputBytes != after.CommandOutputBytes},
		{"delivery.commit_title_template", before.CommitTitleTemplate != after.CommitTitleTemplate}, {"delivery.commit_body_template", before.CommitBodyTemplate != after.CommitBodyTemplate},
		{"delivery.pull_request_title_template", before.PullRequestTitleTemplate != after.PullRequestTitleTemplate}, {"delivery.pull_request_body_template", before.PullRequestBodyTemplate != after.PullRequestBodyTemplate}}
	for _, v := range values {
		if v.changed {
			origins[v.key] = origin
		}
	}
}

func markFixListOrigins(origins map[string]appconfig.Origin, before, after preferences.Fix, origin appconfig.Origin) {
	prior := make(map[string]struct{}, len(before.Focus))
	for _, metric := range before.Focus {
		prior[metric] = struct{}{}
	}
	for _, metric := range after.Focus {
		if _, exists := prior[metric]; !exists {
			origins["fix.focus."+metric] = origin
		}
	}
}
