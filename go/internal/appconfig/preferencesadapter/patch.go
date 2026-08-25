package preferencesadapter

import (
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/validation"
)

func applyUserPatch(value *preferences.Document, patch appconfig.Patch) error {
	if patch.Fix != nil {
		value.Fix = appFixToPreference(*patch.Fix)
	}
	if patch.Concurrency != nil {
		value.Concurrency = appConcurrencyToPreference(*patch.Concurrency)
	}
	if patch.Profiles != nil {
		value.Agents.Profiles = appProfilesToPreference(*patch.Profiles)
	}
	if patch.Validation != nil {
		value.Validation.Plans = appPlansToPreference(*patch.Validation)
	}
	if patch.ValidationWorkspace != nil {
		value.ValidationWorkspace = appValidationWorkspaceToPreference(*patch.ValidationWorkspace)
	}
	if patch.Delivery != nil {
		value.Delivery = appDeliveryToPreference(*patch.Delivery)
	}
	if patch.TrendWindow != nil {
		value.Interaction.TrendWindow = patch.TrendWindow.String()
	}
	return nil
}

func applyRepositoryPatch(value *preferences.PartialDocument, patch appconfig.Patch) error {
	if patch.Profiles != nil {
		return errors.New("repository preferences cannot register agent profiles or authentication references")
	}
	if patch.Fix != nil {
		item := appFixToPreference(*patch.Fix)
		value.Fix = &item
	}
	if patch.Concurrency != nil {
		item := appConcurrencyToPreference(*patch.Concurrency)
		value.Concurrency = &item
	}
	if patch.Validation != nil {
		for _, plan := range *patch.Validation {
			if len(plan.Checks) != 0 {
				return errors.New("repository preferences may select validation plan IDs but cannot define executable checks")
			}
		}
		item := preferences.Validation{Plans: appPlansToPreference(*patch.Validation)}
		value.Validation = &item
	}
	if patch.ValidationWorkspace != nil {
		return errors.New("repository preferences cannot override user-owned validation workspace limits")
	}
	if patch.Delivery != nil {
		item := appDeliveryToPreference(*patch.Delivery)
		value.Delivery = &item
	}
	if patch.TrendWindow != nil {
		item := preferences.Interaction{TrendWindow: patch.TrendWindow.String()}
		value.Interaction = &item
	}
	return nil
}

func (adapter *Adapter) applyRepository(resolved *appconfig.Resolved, value preferences.PartialDocument) error {
	if err := validateRepositoryPartial(value); err != nil {
		return err
	}
	// Repository preferences are untrusted input from the checked-out tree.
	// Compare them with the already resolved user configuration before applying
	// them so precedence can only narrow authority and bounded resource use.
	if err := validateRepositoryOverride(*resolved, value); err != nil {
		return err
	}
	if value.Fix != nil {
		converted, err := preferenceFixToApp(*value.Fix)
		if err != nil {
			return err
		}
		resolved.Fix = converted
		resolved.Origins["fix"] = appconfig.OriginRepository
		setOrigins(resolved.Origins, appconfig.OriginRepository, "fix.target_score", "fix.focus", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.delegation", "fix.max_attempts", "fix.attempt_timeout", "fix.prompt_template", "fix.branch_template", "fix.validation_plan")
		markFixListOrigins(resolved.Origins, preferences.Fix{}, *value.Fix, appconfig.OriginRepository)
	}
	if value.Concurrency != nil {
		resolved.Concurrency = preferenceConcurrencyToApp(*value.Concurrency)
		resolved.Origins["concurrency"] = appconfig.OriginRepository
		setOrigins(resolved.Origins, appconfig.OriginRepository, "concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_retained_jobs", "concurrency.max_transcript_bytes", "concurrency.max_actors_per_job", "concurrency.max_candidate_preview_bytes", "concurrency.max_candidate_preview_lines")
	}
	if value.Validation != nil {
		selected, err := selectValidationPlans(resolved.Validation, value.Validation.Plans)
		if err != nil {
			return err
		}
		resolved.Validation = selected
		resolved.Origins["validation"] = appconfig.OriginRepository
		for _, plan := range selected {
			resolved.Origins["validation."+plan.ID] = appconfig.OriginRepository
		}
	}
	if value.Delivery != nil {
		resolved.Delivery = preferenceDeliveryToApp(*value.Delivery)
		resolved.Origins["delivery"] = appconfig.OriginRepository
		setOrigins(resolved.Origins, appconfig.OriginRepository, "delivery.default_mode", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests", "delivery.require_validation", "delivery.command_output_bytes")
	}
	if value.Interaction != nil {
		trend, err := parseDuration("interaction.trend_window", value.Interaction.TrendWindow)
		if err != nil {
			return err
		}
		resolved.TrendWindow = trend
		resolved.Origins["interaction.trend_window"] = appconfig.OriginRepository
	}
	if err := adapter.validateResolved(*resolved); err != nil {
		return err
	}
	return nil
}

func setOrigins(values map[string]appconfig.Origin, origin appconfig.Origin, keys ...string) {
	for _, key := range keys {
		values[key] = origin
	}
}

func selectValidationPlans(available []validation.Plan, requested []preferences.ValidationPlan) ([]validation.Plan, error) {
	byID := make(map[string]validation.Plan, len(available))
	for _, plan := range available {
		byID[plan.ID] = plan
	}
	result := make([]validation.Plan, len(requested))
	for index, request := range requested {
		plan, ok := byID[request.ID]
		if !ok {
			return nil, fmt.Errorf("repository selects unknown validation plan %q", request.ID)
		}
		result[index] = clonePlan(plan)
	}
	return result, nil
}

func applyOverrides(value *appconfig.Resolved, overrides appconfig.SessionOverrides) {
	if overrides.TargetScore != nil {
		value.Fix.TargetScore = *overrides.TargetScore
		value.Origins["fix.target_score"] = appconfig.OriginSession
	}
	if overrides.Profile != nil {
		value.Fix.Profile = *overrides.Profile
		value.Origins["fix.profile"] = appconfig.OriginSession
	}
	if overrides.Model != nil {
		value.Fix.Model = *overrides.Model
		value.Origins["fix.model"] = appconfig.OriginSession
	}
	if overrides.Effort != nil {
		value.Fix.Effort = *overrides.Effort
		value.Origins["fix.effort"] = appconfig.OriginSession
	}
	if overrides.TrendWindow != nil {
		value.TrendWindow = *overrides.TrendWindow
		value.Origins["interaction.trend_window"] = appconfig.OriginSession
	}
}
