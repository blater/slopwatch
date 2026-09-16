package preferencesadapter

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/scoring"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type profileValidator struct {
	runtimeKinds map[agent.RuntimeKind]struct{}
	catalog      agent.ProfileCatalog
}

func validateDocument(value preferences.Document, validator profileValidator) error {
	resolved, err := documentToResolved(value)
	if err != nil {
		return err
	}
	return validateResolved(resolved, validator)
}

func validateResolved(value appconfig.Resolved, validator profileValidator) error {
	if err := validateFix(value.Fix); err != nil {
		return err
	}
	if err := validateConcurrency(value.Concurrency); err != nil {
		return err
	}
	if err := validateDelivery(value.Delivery); err != nil {
		return err
	}
	profiles, err := validator.validateProfiles(value.Profiles)
	if err != nil {
		return err
	}
	if err := validateProfileSelection(value.Fix.Profile, profiles); err != nil {
		return err
	}
	if value.TrendWindow <= 0 {
		return fmt.Errorf("interaction trend window must be greater than zero")
	}
	return nil
}

func (validator profileValidator) validateProfiles(values []agent.Profile) (map[agent.ProfileID]struct{}, error) {
	profiles := make(map[agent.ProfileID]struct{}, len(values))
	for _, profile := range values {
		if profile.ID == "" {
			return nil, fmt.Errorf("agent profile ID cannot be empty")
		}
		if _, exists := profiles[profile.ID]; exists {
			return nil, fmt.Errorf("agent profile ID %q is duplicated", profile.ID)
		}
		profiles[profile.ID] = struct{}{}
		if _, known := validator.runtimeKinds[profile.Runtime]; !known {
			return nil, fmt.Errorf("agent profile %q uses unknown runtime %q", profile.ID, profile.Runtime)
		}
		if err := validateTrustedText("agent executable", profile.Executable); err != nil {
			return nil, fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
		if err := validator.validateExecutable(profile); err != nil {
			return nil, err
		}
		if err := validateAuthReference(profile.AuthenticationRef); err != nil {
			return nil, fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
		for key, option := range profile.Options {
			if option != "" && sensitiveOptionKey(key) {
				return nil, fmt.Errorf("agent profile %q option %q cannot contain a literal credential; use an authentication reference", profile.ID, key)
			}
		}
		if validator.catalog != nil {
			if err := validator.catalog.ValidateProfile(profile); err != nil {
				return nil, fmt.Errorf("agent profile %q: %w", profile.ID, err)
			}
		}
	}
	return profiles, nil
}

func (validator profileValidator) validateExecutable(profile agent.Profile) error {
	if profile.Executable != "" {
		return nil
	}
	requiresExecutable := validator.catalog == nil
	if validator.catalog != nil {
		descriptor, err := validator.catalog.Descriptor(profile.Runtime)
		if err != nil {
			return fmt.Errorf("agent profile %q: %w", profile.ID, err)
		}
		for _, field := range descriptor.Fields {
			requiresExecutable = requiresExecutable || (field.Key == "executable" && field.Required)
		}
	}
	if requiresExecutable {
		return fmt.Errorf("agent profile %q executable cannot be empty", profile.ID)
	}
	return nil
}

func validateProfileSelection(profile agent.ProfileID, profiles map[agent.ProfileID]struct{}) error {
	if profile != "" {
		if _, exists := profiles[profile]; !exists {
			return fmt.Errorf("fix profile %q is not configured", profile)
		}
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
	if strings.TrimSpace(value.PromptTemplate) == "" {
		return errors.New("fix prompt template cannot be empty")
	}
	if strings.ContainsRune(value.PromptTemplate, '\x00') {
		return errors.New("fix prompt template contains a NUL character")
	}
	seen := make(map[scoring.MetricID]struct{}, len(value.Focus))
	for _, metric := range value.Focus {
		id := scoring.MetricID(metric)
		if _, ok := scoring.MetricDefinitionByID(id); !ok && fix.MetricID(metric) != fix.MetricScore {
			return fmt.Errorf("unknown fix focus metric %q", metric)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("fix focus metric %q is duplicated", metric)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateConcurrency(value appconfig.Concurrency) error {
	if value.MaxAgents <= 0 || value.MaxVerifiers <= 0 || value.MaxActorsPerJob <= 0 ||
		value.MaxCandidatePreviewBytes <= 0 || value.MaxCandidatePreviewLines <= 0 {
		return fmt.Errorf("concurrency limits must all be greater than zero")
	}
	return nil
}

func validateDelivery(value appconfig.Delivery) error {
	if !value.DefaultPlan.Valid() {
		return fmt.Errorf("unsupported delivery plan %+v", value.DefaultPlan)
	}
	if value.Publisher != "github-cli" {
		return fmt.Errorf("unsupported pull-request publisher %q", value.Publisher)
	}
	if value.DefaultPlan.Publish == fix.PublishPullRequest && strings.TrimSpace(value.BaseBranch) == "" {
		return fmt.Errorf("pull-request delivery requires an explicit base branch")
	}
	if value.CommandOutputBytes <= 0 {
		return fmt.Errorf("delivery command output budget must be greater than zero")
	}
	if err := appconfig.ValidateBranchTemplate(value.BranchTemplate); err != nil {
		return fmt.Errorf("delivery %w", err)
	}
	for label, text := range map[string]string{"commit title template": value.CommitTitleTemplate, "commit body template": value.CommitBodyTemplate, "pull request title template": value.PullRequestTitleTemplate, "pull request body template": value.PullRequestBodyTemplate} {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("delivery %s cannot be empty", label)
		}
		if err := validateTrustedText(label, text); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedText(label, value string) error {
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s contains invalid control characters", label)
		}
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
