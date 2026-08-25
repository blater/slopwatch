package preferencesadapter

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/validation"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (adapter *Adapter) validateDocument(value preferences.Document) error {
	resolved, err := documentToResolved(value)
	if err != nil {
		return err
	}
	return adapter.validateResolved(resolved)
}

func (adapter *Adapter) validateResolved(value appconfig.Resolved) error {
	if err := validateFix(value.Fix); err != nil {
		return err
	}
	if value.Concurrency.MaxAgents <= 0 || value.Concurrency.MaxVerifiers <= 0 ||
		value.Concurrency.MaxRetainedJobs <= 0 || value.Concurrency.MaxTranscriptBytes <= 0 {
		return fmt.Errorf("concurrency limits must all be greater than zero")
	}
	if !value.Delivery.DefaultMode.Valid() {
		return fmt.Errorf("unsupported delivery mode %q", value.Delivery.DefaultMode)
	}
	profiles := make(map[agent.ProfileID]struct{}, len(value.Profiles))
	for _, profile := range value.Profiles {
		if profile.ID == "" {
			return fmt.Errorf("agent profile ID cannot be empty")
		}
		if _, exists := profiles[profile.ID]; exists {
			return fmt.Errorf("agent profile ID %q is duplicated", profile.ID)
		}
		profiles[profile.ID] = struct{}{}
		if _, known := adapter.runtimeKinds[profile.Runtime]; !known {
			return fmt.Errorf("agent profile %q uses unknown runtime %q", profile.ID, profile.Runtime)
		}
		if err := validateTrustedText("agent executable", profile.Executable); err != nil {
			return fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
		if profile.Executable == "" {
			return fmt.Errorf("agent profile %q executable cannot be empty", profile.ID)
		}
		if err := validateAuthReference(profile.AuthenticationRef); err != nil {
			return fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
		for key, option := range profile.Options {
			if option != "" && sensitiveOptionKey(key) {
				return fmt.Errorf("agent profile %q option %q cannot contain a literal credential; use an authentication reference", profile.ID, key)
			}
		}
		// Provider validation applies to the complete profile, including profiles
		// with no adapter options. Keeping this outside the option loop prevents an
		// invalid auth/runtime combination from bypassing its owning strategy.
		if adapter.profileCatalog != nil {
			if err := adapter.profileCatalog.ValidateProfile(profile); err != nil {
				return fmt.Errorf("agent profile %q: %w", profile.ID, err)
			}
		}
	}
	if value.Fix.Profile != "" {
		if _, exists := profiles[value.Fix.Profile]; !exists {
			return fmt.Errorf("fix profile %q is not configured", value.Fix.Profile)
		}
	}
	if err := validatePlans(value.Validation); err != nil {
		return err
	}
	if value.Fix.ValidationPlan != "" {
		found := false
		for _, plan := range value.Validation {
			found = found || plan.ID == value.Fix.ValidationPlan
		}
		if !found {
			return fmt.Errorf("fix validation plan %q is not configured", value.Fix.ValidationPlan)
		}
	}
	if value.TrendWindow <= 0 {
		return fmt.Errorf("interaction trend window must be greater than zero")
	}
	return nil
}

func validateFix(value appconfig.FixDefaults) error {
	if math.IsNaN(value.TargetScore) || math.IsInf(value.TargetScore, 0) || value.TargetScore < 0 {
		return fmt.Errorf("fix target score must be finite and non-negative")
	}
	switch value.ChangeScope {
	case "targets-only", "targets-and-tests", "repository":
	default:
		return fmt.Errorf("unsupported fix change scope %q", value.ChangeScope)
	}
	if value.Delegation == "" {
		return fmt.Errorf("fix delegation mode cannot be empty")
	}
	if value.MaxAttempts <= 0 || value.AttemptTimeout <= 0 {
		return fmt.Errorf("fix attempts and timeout must be greater than zero")
	}
	seen := make(map[scoring.MetricID]struct{}, len(value.Focus))
	for _, metric := range value.Focus {
		id := scoring.MetricID(metric)
		if _, ok := scoring.MetricDefinitionByID(id); !ok {
			return fmt.Errorf("unknown fix focus metric %q", metric)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("fix focus metric %q is duplicated", metric)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validatePlans(plans []validation.Plan) error {
	seenPlans := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if plan.ID == "" {
			return fmt.Errorf("validation plan ID cannot be empty")
		}
		if _, exists := seenPlans[plan.ID]; exists {
			return fmt.Errorf("validation plan ID %q is duplicated", plan.ID)
		}
		seenPlans[plan.ID] = struct{}{}
		seenChecks := make(map[validation.CheckID]struct{}, len(plan.Checks))
		for _, check := range plan.Checks {
			if check.ID == "" {
				return fmt.Errorf("validation plan %q has an empty check ID", plan.ID)
			}
			if _, exists := seenChecks[check.ID]; exists {
				return fmt.Errorf("validation plan %q check ID %q is duplicated", plan.ID, check.ID)
			}
			seenChecks[check.ID] = struct{}{}
			if err := validateTrustedText("validation executable", check.Executable); err != nil {
				return fmt.Errorf("validation plan %q check %q: %w", plan.ID, check.ID, err)
			}
			if check.Executable == "" {
				return fmt.Errorf("validation plan %q check %q executable cannot be empty", plan.ID, check.ID)
			}
			for _, argument := range check.Arguments {
				if strings.ContainsRune(argument, 0) || strings.ContainsAny(argument, "\r\n") {
					return fmt.Errorf("validation plan %q check %q has an invalid argument", plan.ID, check.ID)
				}
			}
			if check.Timeout <= 0 || check.MaxOutputBytes <= 0 {
				return fmt.Errorf("validation plan %q check %q timeout and output limit must be greater than zero", plan.ID, check.ID)
			}
		}
	}
	return nil
}

func validateRepositoryPartial(value preferences.PartialDocument) error {
	if value.Agents != nil && len(value.Agents.Profiles) != 0 {
		return fmt.Errorf("repository preferences cannot register agent profiles, commands, or credentials")
	}
	if value.Validation != nil {
		seen := make(map[string]struct{}, len(value.Validation.Plans))
		for _, plan := range value.Validation.Plans {
			if plan.ID == "" {
				return fmt.Errorf("repository validation plan ID cannot be empty")
			}
			if _, exists := seen[plan.ID]; exists {
				return fmt.Errorf("repository validation plan ID %q is duplicated", plan.ID)
			}
			seen[plan.ID] = struct{}{}
			if len(plan.Checks) != 0 {
				return fmt.Errorf("repository preferences may select validation plan IDs but cannot define executable checks")
			}
		}
	}
	return nil
}

func validateTrustedText(label, value string) error {
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s contains invalid control characters", label)
	}
	return nil
}

func validateAuthReference(value string) error {
	switch value {
	case "", "provider", "provider-owned":
		return nil
	}
	if strings.HasPrefix(value, "env:") && environmentName.MatchString(strings.TrimPrefix(value, "env:")) {
		return nil
	}
	if strings.HasPrefix(value, "keychain:") && strings.TrimSpace(strings.TrimPrefix(value, "keychain:")) != "" && !strings.ContainsAny(value, "\r\n\x00") {
		return nil
	}
	return fmt.Errorf("authentication_ref must be provider-owned, env:<variable>, or keychain:<item>; literal credentials are not permitted")
}

func sensitiveOptionKey(value string) bool {
	key := strings.ToLower(strings.ReplaceAll(value, "-", "_"))
	switch key {
	case "secret", "token", "password", "credential", "api_key", "access_token", "auth_token", "client_secret":
		return true
	}
	for _, suffix := range []string{"_secret", "_token", "_password", "_credential"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}
