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
		value.Concurrency.MaxRetainedJobs <= 0 || value.Concurrency.MaxTranscriptBytes <= 0 || value.Concurrency.MaxActorsPerJob <= 0 ||
		value.Concurrency.MaxCandidatePreviewBytes <= 0 || value.Concurrency.MaxCandidatePreviewLines <= 0 {
		return fmt.Errorf("concurrency limits must all be greater than zero")
	}
	if err := validateValidationWorkspace(value.ValidationWorkspace); err != nil {
		return err
	}
	if !value.Delivery.DefaultMode.Valid() {
		return fmt.Errorf("unsupported delivery mode %q", value.Delivery.DefaultMode)
	}
	if value.Delivery.Publisher != "github-cli" {
		return fmt.Errorf("unsupported pull-request publisher %q", value.Delivery.Publisher)
	}
	if value.Delivery.DefaultMode == "pull-request" && strings.TrimSpace(value.Delivery.BaseBranch) == "" {
		return fmt.Errorf("pull-request delivery requires an explicit base branch")
	}
	if value.Delivery.CommandOutputBytes <= 0 {
		return fmt.Errorf("delivery command output budget must be greater than zero")
	}
	if err := appconfig.ValidateBranchTemplate(value.Delivery.BranchTemplate); err != nil {
		return fmt.Errorf("delivery %w", err)
	}
	if value.Delivery.CommitPolicy != "automatic" || value.Delivery.CleanupPolicy != "remove-worktree" {
		return fmt.Errorf("delivery commit/cleanup policy is unsupported")
	}
	for label, text := range map[string]string{"commit title template": value.Delivery.CommitTitleTemplate, "commit body template": value.Delivery.CommitBodyTemplate, "pull request title template": value.Delivery.PullRequestTitleTemplate, "pull request body template": value.Delivery.PullRequestBodyTemplate} {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("delivery %s cannot be empty", label)
		}
		if err := validateTrustedText(label, text); err != nil {
			return err
		}
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
			// Process-backed adapters require an executable through their own
			// descriptor. API-backed adapters deliberately have no process field;
			// imposing one here would leak one adapter's shape into shared config.
			requiresExecutable := adapter.profileCatalog == nil
			if adapter.profileCatalog != nil {
				descriptor, descriptorErr := adapter.profileCatalog.Descriptor(profile.Runtime)
				if descriptorErr != nil {
					return fmt.Errorf("agent profile %q: %w", profile.ID, descriptorErr)
				}
				for _, field := range descriptor.Fields {
					requiresExecutable = requiresExecutable || (field.Key == "executable" && field.Required)
				}
			}
			if requiresExecutable {
				return fmt.Errorf("agent profile %q executable cannot be empty", profile.ID)
			}
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
	if strings.TrimSpace(value.PromptTemplate) == "" {
		return errors.New("fix prompt template cannot be empty")
	}
	if strings.ContainsRune(value.PromptTemplate, '\x00') {
		return errors.New("fix prompt template contains a NUL character")
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

func validateValidationWorkspace(value appconfig.ValidationWorkspace) error {
	if value.MaxFiles <= 0 || value.MaxDirectories <= 0 || value.MaxPathBytes <= 0 || value.MaxFileBytes <= 0 || value.MaxTotalBytes <= 0 {
		return fmt.Errorf("validation workspace limits must all be greater than zero")
	}
	if value.MaxFileBytes > value.MaxTotalBytes {
		return fmt.Errorf("validation workspace max file bytes cannot exceed max total bytes")
	}
	if value.ContainerPIDs <= 0 || value.ContainerMemoryBytes <= 0 || value.ContainerCPUMillis <= 0 || value.ContainerTemporaryBytes <= 0 ||
		value.ContainerWorkspaceBytes <= 0 || value.ContainerNofileLimit <= 0 || value.ContainerGeneratedFileBytes <= 0 ||
		value.ContainerStopTimeout <= 0 || value.ContainerControlTimeout <= 0 || value.ContainerSentinelTimeout <= 0 || value.ContainerCrashProbeTimeout <= 0 {
		return fmt.Errorf("validation container operational limits must all be greater than zero")
	}
	if value.ContainerWorkspaceBytes <= value.MaxTotalBytes {
		return fmt.Errorf("validation container workspace bytes must exceed admitted total bytes")
	}
	if value.ContainerGeneratedFileBytes < value.MaxFileBytes {
		return fmt.Errorf("validation container generated-file bytes must cover admitted max file bytes")
	}
	if value.ContainerControlTimeout <= value.ContainerStopTimeout {
		return fmt.Errorf("validation container control timeout must exceed stop timeout")
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

// validateRepositoryOverride is the repository allowlist. The checked-out
// repository may tighten goals, paths, delivery and bounded resource limits,
// but it cannot choose identities, templates or destinations owned by the
// user. Fields without a provider-independent ordering must remain unchanged.
func validateRepositoryOverride(inherited appconfig.Resolved, value preferences.PartialDocument) error {
	if value.ValidationWorkspace != nil {
		return repositoryOwnedField("validation workspace limits")
	}
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
		if candidate.Delegation != inherited.Fix.Delegation {
			return repositoryOwnedField("fix delegation")
		}
		if candidate.PromptTemplate != inherited.Fix.PromptTemplate {
			return repositoryOwnedField("fix prompt template")
		}
		if inherited.Fix.ValidationPlan != "" && candidate.ValidationPlan != inherited.Fix.ValidationPlan {
			return fmt.Errorf("repository preferences cannot replace or remove inherited fix validation plan %q", inherited.Fix.ValidationPlan)
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
		if candidate.MaxRetainedJobs > inherited.Concurrency.MaxRetainedJobs {
			return repositoryBroadening("concurrency retained jobs", candidate.MaxRetainedJobs, inherited.Concurrency.MaxRetainedJobs)
		}
		if candidate.MaxTranscriptBytes > inherited.Concurrency.MaxTranscriptBytes {
			return repositoryBroadening("concurrency transcript bytes", candidate.MaxTranscriptBytes, inherited.Concurrency.MaxTranscriptBytes)
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
		if deliveryModeRank(candidate.DefaultMode) > deliveryModeRank(inherited.Delivery.DefaultMode) {
			return repositoryBroadening("delivery mode", candidate.DefaultMode, inherited.Delivery.DefaultMode)
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
		if inherited.Delivery.RequireValidation && !candidate.RequireValidation {
			return repositoryBroadening("pull request validation policy", candidate.RequireValidation, inherited.Delivery.RequireValidation)
		}
		if candidate.CommandOutputBytes > inherited.Delivery.CommandOutputBytes {
			return repositoryBroadening("delivery command output bytes", candidate.CommandOutputBytes, inherited.Delivery.CommandOutputBytes)
		}
		if candidate.CommitPolicy != inherited.Delivery.CommitPolicy {
			return repositoryOwnedField("delivery commit policy")
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
		if candidate.CleanupPolicy != inherited.Delivery.CleanupPolicy {
			return repositoryOwnedField("delivery cleanup policy")
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
		// The normal resolved validation reports the unsupported value. Treat it
		// as broad here so it can never slip through a repository comparison.
		return 3
	}
}

func deliveryModeRank(value fix.DeliveryMode) int {
	switch value {
	case fix.DeliveryModeBranch:
		return 0
	case fix.DeliveryModePullRequest:
		return 1
	default:
		return 2
	}
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
