package preferencesadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/validation"
)

func documentToResolved(value preferences.Document) (appconfig.Resolved, error) {
	fixDefaults, err := preferenceFixToApp(value.Fix)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	trend, err := parseDuration("interaction.trend_window", value.Interaction.TrendWindow)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	plans, err := preferencePlansToApp(value.Validation.Plans)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	workspace, err := preferenceValidationWorkspaceToApp(value.ValidationWorkspace)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	return appconfig.Resolved{
		SchemaVersion:       value.Version,
		Fix:                 fixDefaults,
		Concurrency:         preferenceConcurrencyToApp(value.Concurrency),
		Profiles:            preferenceProfilesToApp(value.Agents.Profiles),
		Validation:          plans,
		ValidationWorkspace: workspace,
		Delivery:            preferenceDeliveryToApp(value.Delivery),
		TrendWindow:         trend,
	}, nil
}

func preferenceValidationWorkspaceToApp(value preferences.ValidationWorkspace) (appconfig.ValidationWorkspace, error) {
	stop, err := parseDuration("validation_workspace.container_stop_timeout", value.ContainerStopTimeout)
	if err != nil {
		return appconfig.ValidationWorkspace{}, err
	}
	control, err := parseDuration("validation_workspace.container_control_timeout", value.ContainerControlTimeout)
	if err != nil {
		return appconfig.ValidationWorkspace{}, err
	}
	sentinel, err := parseDuration("validation_workspace.container_sentinel_timeout", value.ContainerSentinelTimeout)
	if err != nil {
		return appconfig.ValidationWorkspace{}, err
	}
	crash, err := parseDuration("validation_workspace.container_crash_probe_timeout", value.ContainerCrashProbeTimeout)
	if err != nil {
		return appconfig.ValidationWorkspace{}, err
	}
	return appconfig.ValidationWorkspace{
		MaxFiles: value.MaxFiles, MaxDirectories: value.MaxDirectories,
		MaxPathBytes: value.MaxPathBytes, MaxFileBytes: value.MaxFileBytes, MaxTotalBytes: value.MaxTotalBytes,
		ContainerPIDs: value.ContainerPIDs, ContainerMemoryBytes: value.ContainerMemoryBytes, ContainerCPUMillis: value.ContainerCPUMillis,
		ContainerTemporaryBytes: value.ContainerTemporaryBytes, ContainerWorkspaceBytes: value.ContainerWorkspaceBytes,
		ContainerNofileLimit: value.ContainerNofileLimit, ContainerGeneratedFileBytes: value.ContainerGeneratedFileBytes,
		ContainerStopTimeout: stop, ContainerControlTimeout: control, ContainerSentinelTimeout: sentinel, ContainerCrashProbeTimeout: crash,
	}, nil
}

func appValidationWorkspaceToPreference(value appconfig.ValidationWorkspace) preferences.ValidationWorkspace {
	return preferences.ValidationWorkspace{
		MaxFiles: value.MaxFiles, MaxDirectories: value.MaxDirectories,
		MaxPathBytes: value.MaxPathBytes, MaxFileBytes: value.MaxFileBytes, MaxTotalBytes: value.MaxTotalBytes,
		ContainerPIDs: value.ContainerPIDs, ContainerMemoryBytes: value.ContainerMemoryBytes, ContainerCPUMillis: value.ContainerCPUMillis,
		ContainerTemporaryBytes: value.ContainerTemporaryBytes, ContainerWorkspaceBytes: value.ContainerWorkspaceBytes,
		ContainerNofileLimit: value.ContainerNofileLimit, ContainerGeneratedFileBytes: value.ContainerGeneratedFileBytes,
		ContainerStopTimeout: value.ContainerStopTimeout.String(), ContainerControlTimeout: value.ContainerControlTimeout.String(),
		ContainerSentinelTimeout: value.ContainerSentinelTimeout.String(), ContainerCrashProbeTimeout: value.ContainerCrashProbeTimeout.String(),
	}
}

func preferenceFixToApp(value preferences.Fix) (appconfig.FixDefaults, error) {
	focus := make([]fix.MetricID, len(value.Focus))
	for index, id := range value.Focus {
		focus[index] = fix.MetricID(id)
	}
	return appconfig.FixDefaults{
		TargetScore: value.TargetScore, Focus: focus, ChangeScope: value.ChangeScope,
		Profile: agent.ProfileID(value.Profile), Model: agent.ModelID(value.Model),
		Effort: agent.EffortID(value.Effort), Delegation: agent.DelegationMode(value.Delegation),
		PromptTemplate: value.PromptTemplate,
		ValidationPlan: value.ValidationPlan,
	}, nil
}

func preferenceProfilesToApp(values []preferences.AgentProfile) []agent.Profile {
	result := make([]agent.Profile, len(values))
	for index, value := range values {
		result[index] = agent.Profile{
			ID: agent.ProfileID(value.ID), Label: value.Label, Runtime: agent.RuntimeKind(value.Runtime),
			Executable: value.Executable, RuntimeProfile: value.RuntimeProfile,
			AuthenticationRef: value.AuthenticationRef, Options: cloneStringMap(value.Options),
		}
		result[index].Fingerprint = profileFingerprint(result[index])
	}
	return result
}

func profileFingerprint(value agent.Profile) string {
	keys := make([]string, 0, len(value.Options))
	for key := range value.Options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var encoded strings.Builder
	for _, field := range []string{string(value.ID), string(value.Runtime), value.Executable, value.RuntimeProfile, value.AuthenticationRef} {
		encoded.WriteString(field)
		encoded.WriteByte(0)
	}
	for _, key := range keys {
		encoded.WriteString(key)
		encoded.WriteByte('=')
		encoded.WriteString(value.Options[key])
		encoded.WriteByte(0)
	}
	digest := sha256.Sum256([]byte(encoded.String()))
	return hex.EncodeToString(digest[:])
}

func preferencePlansToApp(values []preferences.ValidationPlan) ([]validation.Plan, error) {
	result := make([]validation.Plan, len(values))
	for planIndex, value := range values {
		result[planIndex].ID = value.ID
		result[planIndex].Checks = make([]validation.Check, len(value.Checks))
		for checkIndex, check := range value.Checks {
			timeout, err := parseDuration("validation check timeout", check.Timeout)
			if err != nil {
				return nil, fmt.Errorf("plan %q check %q: %w", value.ID, check.ID, err)
			}
			var directory fix.RepoPath
			if check.WorkingDirectory != "" {
				directory, err = fix.ParseRepoPath(check.WorkingDirectory)
				if err != nil {
					return nil, fmt.Errorf("plan %q check %q working directory: %w", value.ID, check.ID, err)
				}
			}
			result[planIndex].Checks[checkIndex] = validation.Check{
				ID: validation.CheckID(check.ID), Label: check.Label,
				Executable: check.Executable, Arguments: append([]string(nil), check.Arguments...),
				WorkingDirectory: directory, Required: check.Required,
				Timeout: timeout, MaxOutputBytes: check.MaxOutputBytes,
			}
		}
	}
	return result, nil
}

func preferenceConcurrencyToApp(value preferences.Concurrency) appconfig.Concurrency {
	return appconfig.Concurrency{
		MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers,
		MaxRetainedJobs: value.MaxRetainedJobs, MaxTranscriptBytes: value.MaxTranscriptBytes,
		MaxActorsPerJob:          value.MaxActorsPerJob,
		MaxCandidatePreviewBytes: value.MaxCandidatePreviewBytes,
		MaxCandidatePreviewLines: value.MaxCandidatePreviewLines,
	}
}

func preferenceDeliveryToApp(value preferences.Delivery) appconfig.Delivery {
	return appconfig.Delivery{
		DefaultMode: fix.DeliveryMode(value.DefaultMode), Remote: value.Remote, BaseBranch: value.BaseBranch,
		BranchTemplate: value.BranchTemplate, Publisher: value.Publisher,
		DraftPullRequests: value.DraftPullRequests, RequireValidation: value.RequireValidation,
		CommandOutputBytes: value.CommandOutputBytes,
		CommitPolicy:       value.CommitPolicy, CommitTitleTemplate: value.CommitTitleTemplate, CommitBodyTemplate: value.CommitBodyTemplate,
		PullRequestTitleTemplate: value.PullRequestTitleTemplate, PullRequestBodyTemplate: value.PullRequestBodyTemplate, CleanupPolicy: value.CleanupPolicy,
	}
}

func appFixToPreference(value appconfig.FixDefaults) preferences.Fix {
	focus := make([]string, len(value.Focus))
	for index, id := range value.Focus {
		focus[index] = string(id)
	}
	return preferences.Fix{
		TargetScore: value.TargetScore, Focus: focus, ChangeScope: value.ChangeScope,
		Profile: string(value.Profile), Model: string(value.Model), Effort: string(value.Effort),
		Delegation: string(value.Delegation), PromptTemplate: value.PromptTemplate,
		ValidationPlan: value.ValidationPlan,
	}
}

func appProfilesToPreference(values []agent.Profile) []preferences.AgentProfile {
	result := make([]preferences.AgentProfile, len(values))
	for index, value := range values {
		result[index] = preferences.AgentProfile{
			ID: string(value.ID), Label: value.Label, Runtime: string(value.Runtime),
			Executable: value.Executable, RuntimeProfile: value.RuntimeProfile,
			AuthenticationRef: value.AuthenticationRef, Options: cloneStringMap(value.Options),
		}
	}
	return result
}

func appPlansToPreference(values []validation.Plan) []preferences.ValidationPlan {
	result := make([]preferences.ValidationPlan, len(values))
	for planIndex, value := range values {
		result[planIndex].ID = value.ID
		result[planIndex].Checks = make([]preferences.ValidationCheck, len(value.Checks))
		for checkIndex, check := range value.Checks {
			result[planIndex].Checks[checkIndex] = preferences.ValidationCheck{
				ID: string(check.ID), Label: check.Label, Executable: check.Executable,
				Arguments:        append([]string(nil), check.Arguments...),
				WorkingDirectory: check.WorkingDirectory.String(), Required: check.Required,
				Timeout: check.Timeout.String(), MaxOutputBytes: check.MaxOutputBytes,
			}
		}
	}
	return result
}

func appConcurrencyToPreference(value appconfig.Concurrency) preferences.Concurrency {
	return preferences.Concurrency{
		MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers,
		MaxRetainedJobs: value.MaxRetainedJobs, MaxTranscriptBytes: value.MaxTranscriptBytes,
		MaxActorsPerJob:          value.MaxActorsPerJob,
		MaxCandidatePreviewBytes: value.MaxCandidatePreviewBytes,
		MaxCandidatePreviewLines: value.MaxCandidatePreviewLines,
	}
}

func appDeliveryToPreference(value appconfig.Delivery) preferences.Delivery {
	return preferences.Delivery{
		DefaultMode: string(value.DefaultMode), Remote: value.Remote, BaseBranch: value.BaseBranch,
		BranchTemplate: value.BranchTemplate, Publisher: value.Publisher,
		DraftPullRequests: value.DraftPullRequests, RequireValidation: value.RequireValidation,
		CommandOutputBytes: value.CommandOutputBytes,
		CommitPolicy:       value.CommitPolicy, CommitTitleTemplate: value.CommitTitleTemplate, CommitBodyTemplate: value.CommitBodyTemplate,
		PullRequestTitleTemplate: value.PullRequestTitleTemplate, PullRequestBodyTemplate: value.PullRequestBodyTemplate, CleanupPolicy: value.CleanupPolicy,
	}
}

func parseDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", field, value, err)
	}
	return duration, nil
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
