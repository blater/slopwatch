package follow

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
)

func validateConfigSettings(kind configSettingsKind, value appconfig.Resolved) error {
	return validateConfigSettingsWithCatalog(kind, value, nil)
}

func validateConfigSettingsWithCatalog(kind configSettingsKind, value appconfig.Resolved, catalog agent.ProfileCatalog) error {
	switch kind {
	case configAgents:
		return validateAgentProfiles(value.Profiles, catalog)
	case configFix:
		if math.IsNaN(value.Fix.TargetScore) || math.IsInf(value.Fix.TargetScore, 0) || value.Fix.TargetScore < 0 {
			return errors.New("target score must be finite and non-negative")
		}
	case configConcurrency:
		if value.Concurrency.MaxAgents <= 0 || value.Concurrency.MaxVerifiers <= 0 || value.Concurrency.MaxActorsPerJob <= 0 || value.Concurrency.MaxCandidatePreviewBytes <= 0 || value.Concurrency.MaxCandidatePreviewLines <= 0 {
			return errors.New("all concurrency limits must be positive")
		}
	case configDelivery:
		return validateDeliverySettings(value.Delivery)
	}
	return nil
}

func validateAgentProfiles(profiles []agent.Profile, catalog agent.ProfileCatalog) error {
	seen, runtimes := map[agent.ProfileID]bool{}, map[agent.RuntimeKind]bool{}
	for _, profile := range profiles {
		if profile.ID == "" || profile.Runtime == "" {
			return errors.New("every agent profile needs an ID and runtime")
		}
		if seen[profile.ID] {
			return fmt.Errorf("duplicate agent profile %q", profile.ID)
		}
		seen[profile.ID] = true
		if runtimes[profile.Runtime] {
			return fmt.Errorf("multiple agent profiles use runtime %q; this release supports one account per provider", profile.Runtime)
		}
		runtimes[profile.Runtime] = true
		if err := validateAgentProfile(profile, catalog); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentProfile(profile agent.Profile, catalog agent.ProfileCatalog) error {
	if catalog == nil {
		if profile.Runtime == "codex-cli" && profile.Executable == "" {
			return fmt.Errorf("agent profile %q needs an executable", profile.ID)
		}
		return nil
	}
	descriptor, err := catalog.Descriptor(profile.Runtime)
	if err != nil {
		return fmt.Errorf("agent profile %q: %w", profile.ID, err)
	}
	for _, field := range descriptor.Fields {
		if err := validateProfileFieldValue(field, profileFieldValue(profile, field)); err != nil {
			return fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
	}
	if err := catalog.ValidateProfile(profile); err != nil {
		return fmt.Errorf("agent profile %q: %w", profile.ID, err)
	}
	return nil
}

func validateDeliverySettings(value appconfig.Delivery) error {
	if !value.DefaultPlan.Valid() {
		return errors.New("delivery defaults are inconsistent")
	}
	if value.DefaultPlan.Publish == fix.PublishPullRequest && strings.TrimSpace(value.BaseBranch) == "" {
		return errors.New("pull-request delivery requires an explicit base branch")
	}
	if value.CommandOutputBytes <= 0 {
		return errors.New("Git and publisher output bytes must be positive")
	}
	if value.Publisher != "github-cli" {
		return errors.New("configured pull-request publisher is unavailable")
	}
	return appconfig.ValidateBranchTemplate(value.BranchTemplate)
}
