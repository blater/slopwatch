package preferencesadapter

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/appconfig"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/preferences"
	"github.com/blater/slopmochi/internal/scoring"
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
	if value.Concurrency.MaxAgents <= 0 || value.Concurrency.MaxVerifiers <= 0 || value.Concurrency.MaxActorsPerJob <= 0 ||
		value.Concurrency.MaxCandidatePreviewBytes <= 0 || value.Concurrency.MaxCandidatePreviewLines <= 0 {
		return fmt.Errorf("concurrency limits must all be greater than zero")
	}
	if !value.Delivery.DefaultPlan.Valid() {
		return fmt.Errorf("unsupported delivery plan %+v", value.Delivery.DefaultPlan)
	}
	if value.Delivery.Publisher != "github-cli" {
		return fmt.Errorf("unsupported pull-request publisher %q", value.Delivery.Publisher)
	}
	if value.Delivery.DefaultPlan.Publish == fix.PublishPullRequest && strings.TrimSpace(value.Delivery.BaseBranch) == "" {
		return fmt.Errorf("pull-request delivery requires an explicit base branch")
	}
	if value.Delivery.CommandOutputBytes <= 0 {
		return fmt.Errorf("delivery command output budget must be greater than zero")
	}
	if err := appconfig.ValidateBranchTemplate(value.Delivery.BranchTemplate); err != nil {
		return fmt.Errorf("delivery %w", err)
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
