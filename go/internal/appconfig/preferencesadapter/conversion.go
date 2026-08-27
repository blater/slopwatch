package preferencesadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/appconfig"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/preferences"
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
	return appconfig.Resolved{
		SchemaVersion: value.Version,
		Fix:           fixDefaults,
		Concurrency:   preferenceConcurrencyToApp(value.Concurrency),
		Profiles:      preferenceProfilesToApp(value.Agents.Profiles),
		Delivery:      preferenceDeliveryToApp(value.Delivery),
		TrendWindow:   trend,
	}, nil
}

func preferenceFixToApp(value preferences.Fix) (appconfig.FixDefaults, error) {
	focus := make([]fix.MetricID, len(value.Focus))
	for index, id := range value.Focus {
		focus[index] = fix.MetricID(id)
	}
	return appconfig.FixDefaults{
		TargetScore: value.TargetScore, Focus: focus, ChangeScope: value.ChangeScope,
		Profile: agent.ProfileID(value.Profile), Model: agent.ModelID(value.Model),
		Effort:         agent.EffortID(value.Effort),
		PromptTemplate: value.PromptTemplate,
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

func preferenceConcurrencyToApp(value preferences.Concurrency) appconfig.Concurrency {
	return appconfig.Concurrency{
		MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers,
		MaxActorsPerJob:          value.MaxActorsPerJob,
		MaxCandidatePreviewBytes: value.MaxCandidatePreviewBytes,
		MaxCandidatePreviewLines: value.MaxCandidatePreviewLines,
	}
}

func preferenceDeliveryToApp(value preferences.Delivery) appconfig.Delivery {
	return appconfig.Delivery{
		DefaultPlan: fix.DeliveryPlan{Workspace: fix.WorkspaceMode(value.Workspace), Git: fix.GitMode(value.Git), Publish: fix.PublishMode(value.Publish)}, Remote: value.Remote, BaseBranch: value.BaseBranch,
		BranchTemplate: value.BranchTemplate, Publisher: value.Publisher,
		DraftPullRequests:   value.DraftPullRequests,
		CommandOutputBytes:  value.CommandOutputBytes,
		CommitTitleTemplate: value.CommitTitleTemplate, CommitBodyTemplate: value.CommitBodyTemplate,
		PullRequestTitleTemplate: value.PullRequestTitleTemplate, PullRequestBodyTemplate: value.PullRequestBodyTemplate,
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
		PromptTemplate: value.PromptTemplate,
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

func appConcurrencyToPreference(value appconfig.Concurrency) preferences.Concurrency {
	return preferences.Concurrency{
		MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers,
		MaxActorsPerJob:          value.MaxActorsPerJob,
		MaxCandidatePreviewBytes: value.MaxCandidatePreviewBytes,
		MaxCandidatePreviewLines: value.MaxCandidatePreviewLines,
	}
}

func appDeliveryToPreference(value appconfig.Delivery) preferences.Delivery {
	return preferences.Delivery{
		Workspace: string(value.DefaultPlan.Workspace), Git: string(value.DefaultPlan.Git), Publish: string(value.DefaultPlan.Publish), Remote: value.Remote, BaseBranch: value.BaseBranch,
		BranchTemplate: value.BranchTemplate, Publisher: value.Publisher,
		DraftPullRequests:   value.DraftPullRequests,
		CommandOutputBytes:  value.CommandOutputBytes,
		CommitTitleTemplate: value.CommitTitleTemplate, CommitBodyTemplate: value.CommitBodyTemplate,
		PullRequestTitleTemplate: value.PullRequestTitleTemplate, PullRequestBodyTemplate: value.PullRequestBodyTemplate,
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
