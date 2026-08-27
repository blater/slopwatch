package preferencesadapter

import (
	"errors"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/preferences"
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
		setOrigins(resolved.Origins, appconfig.OriginRepository, "fix.target_score", "fix.focus", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.prompt_template")
		markFixListOrigins(resolved.Origins, preferences.Fix{}, *value.Fix, appconfig.OriginRepository)
	}
	if value.Concurrency != nil {
		resolved.Concurrency = preferenceConcurrencyToApp(*value.Concurrency)
		resolved.Origins["concurrency"] = appconfig.OriginRepository
		setOrigins(resolved.Origins, appconfig.OriginRepository, "concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_actors_per_job", "concurrency.max_candidate_preview_bytes", "concurrency.max_candidate_preview_lines")
	}
	if value.Delivery != nil {
		resolved.Delivery = preferenceDeliveryToApp(*value.Delivery)
		resolved.Origins["delivery"] = appconfig.OriginRepository
		setOrigins(resolved.Origins, appconfig.OriginRepository, "delivery.workspace", "delivery.git", "delivery.publish", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests", "delivery.command_output_bytes")
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
