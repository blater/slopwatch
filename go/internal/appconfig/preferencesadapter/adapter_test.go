package preferencesadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/appconfig"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/preferences"
)

func TestResolveDefaultsAndCreatesUserPreferences(t *testing.T) {
	t.Parallel()
	adapter, userPath, workspace := newTestAdapter(t)
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fix.TargetScore != 100 || resolved.Concurrency.MaxAgents != 2 || resolved.Concurrency.MaxVerifiers != 1 {
		t.Fatalf("resolved defaults = %#v", resolved)
	}
	if resolved.Origins["fix"] != appconfig.OriginBuiltIn || resolved.Revision == 0 {
		t.Fatalf("origins/revision = %#v / %d", resolved.Origins, resolved.Revision)
	}
	if _, err := os.Stat(userPath); err != nil {
		t.Fatalf("user preferences were not created: %v", err)
	}
}

func TestUserRoundTripReturnsDeepCopies(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{
		ID: "primary", Label: "Codex", Runtime: "codex", Executable: "/usr/bin/codex",
		AuthenticationRef: "env:CODEX_API_TOKEN", Options: map[string]string{"sandbox": "strict"},
	}}
	fixDefaults := initial.Fix
	fixDefaults.Profile = "primary"
	fixDefaults.Focus = []fix.MetricID{"cog", "coupling"}
	saved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{
		Profiles: &profiles, Fix: &fixDefaults,
	}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Resolved.Fix.Profile != "primary" || saved.Resolved.Origins["agents"] != appconfig.OriginUser {
		t.Fatalf("saved = %#v", saved.Resolved)
	}
	saved.Resolved.Profiles[0].Options["sandbox"] = "mutated"
	saved.Resolved.Fix.Focus[0] = "npath"
	again, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Profiles[0].Options["sandbox"] != "strict" || again.Fix.Focus[0] != "cog" {
		t.Fatalf("resolved values alias a prior snapshot: %#v", again)
	}
}

func TestUnknownRuntimeIsRejected(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "other", Runtime: "unknown", Executable: "other"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "unknown runtime") {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestAPIBackedProfileMayOmitExecutableWhenDescriptorOwnsThatShape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	defaults := preferences.DefaultDocument()
	defaults.Agents.Profiles = []preferences.AgentProfile{{
		ID: "gpt", Label: "GPT", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY",
	}}
	defaults.Fix.Profile = "gpt"
	catalog := &apiProfileCatalog{}
	adapter, err := New(Options{
		UserPath: filepath.Join(root, "preferences.toml"), Defaults: defaults,
		RuntimeKinds: catalog.Kinds(), ProfileCatalog: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Profiles) != 1 || resolved.Profiles[0].Executable != "" || resolved.Profiles[0].Runtime != "openai-responses" {
		t.Fatalf("resolved API profile = %#v", resolved.Profiles)
	}
}

func TestSaveRejectsStaleRevisionIncludingExternalEdit(t *testing.T) {
	t.Parallel()
	adapter, userPath, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	concurrency := initial.Concurrency
	concurrency.MaxAgents = 3
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, initial.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, initial.Revision); !errors.Is(err, appconfig.ErrRevisionConflict) {
		t.Fatalf("stale Save() error = %v", err)
	}
	current, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n# external editor\n")...)
	if err := os.WriteFile(userPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, current.Revision)
	if !errors.Is(err, appconfig.ErrRevisionConflict) {
		t.Fatalf("externally stale Save() error = %v", err)
	}
}

func TestRepositoryScopeCannotPersistCommandsOrSecrets(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "bad", Runtime: "codex", Executable: "codex", AuthenticationRef: "literal-secret"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "cannot register agent profiles") {
		t.Fatalf("repository profile Save() error = %v", err)
	}
	repositoryPath, _ := adapter.repositoryPreferencesPath(workspace)
	if err := os.MkdirAll(filepath.Dir(repositoryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "version = 1\n[[agents.profiles]]\nid='bad'\nruntime='codex'\nexecutable='sh'\nauthentication_ref='literal-secret'\n"
	if err := os.WriteFile(repositoryPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{}); err != nil {
		t.Fatalf("invalid repository preferences disabled resolution: %v", err)
	}
	if _, err := os.Stat(repositoryPath + ".invalid"); err != nil {
		t.Fatalf("invalid repository preferences were not set aside: %v", err)
	}
}

func TestInvalidRepositoryPatchIsNotPersisted(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := initial.Concurrency
	invalid.MaxAgents = 0
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Concurrency: &invalid}, initial.Revision); err == nil {
		t.Fatal("invalid repository patch was accepted")
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatalf("invalid repository patch was persisted: %v", err)
	}
	if resolved.Concurrency.MaxAgents != initial.Concurrency.MaxAgents {
		t.Fatalf("MaxAgents = %d, want %d", resolved.Concurrency.MaxAgents, initial.Concurrency.MaxAgents)
	}
}

func TestRepositoryNarrowingIsApplied(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	userFix := initial.Fix
	userFix.Focus = []fix.MetricID{"cog"}
	userFix.ChangeScope = "repository"
	userConcurrency := appconfig.Concurrency{
		MaxAgents: 8, MaxVerifiers: 4, MaxActorsPerJob: 32, MaxCandidatePreviewBytes: 8 << 20, MaxCandidatePreviewLines: 10000,
	}
	userDelivery := initial.Delivery
	userDelivery.DefaultPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitCommitNewBranch, Publish: fix.PublishPullRequest}
	userSaved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{
		Fix: &userFix, Concurrency: &userConcurrency, Delivery: &userDelivery,
	}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}

	repositoryFix := userFix
	repositoryFix.TargetScore = 80
	repositoryFix.Focus = []fix.MetricID{"cog", "coupling"}
	repositoryFix.ChangeScope = "targets-only"
	repositoryConcurrency := appconfig.Concurrency{
		MaxAgents: 3, MaxVerifiers: 2, MaxActorsPerJob: 16, MaxCandidatePreviewBytes: 2 << 20, MaxCandidatePreviewLines: 2000,
	}
	repositoryDelivery := userDelivery
	repositoryDelivery.DefaultPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}
	saved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{
		Fix: &repositoryFix, Concurrency: &repositoryConcurrency, Delivery: &repositoryDelivery,
	}, userSaved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Resolved.Fix.TargetScore != 80 || saved.Resolved.Fix.ChangeScope != "targets-only" ||
		saved.Resolved.Concurrency != repositoryConcurrency ||
		saved.Resolved.Delivery.DefaultPlan != repositoryDelivery.DefaultPlan {
		t.Fatalf("repository narrowing was not applied: %#v", saved.Resolved)
	}
	for _, key := range []string{"fix", "concurrency", "delivery"} {
		if saved.Resolved.Origins[key] != appconfig.OriginRepository {
			t.Errorf("origin[%q] = %q, want repository", key, saved.Resolved.Origins[key])
		}
	}
}

func TestRepositoryBroadeningAllowlist(t *testing.T) {
	t.Parallel()
	defaults := preferences.DefaultDocument()
	inherited, err := documentToResolved(defaults)
	if err != nil {
		t.Fatal(err)
	}
	inherited.Fix.Focus = []fix.MetricID{"cog"}
	inherited.Fix.ChangeScope = "targets-and-tests"

	fixOverride := func(change func(*appconfig.FixDefaults)) preferences.PartialDocument {
		candidate := inherited.Fix
		change(&candidate)
		converted := appFixToPreference(candidate)
		return preferences.PartialDocument{Fix: &converted}
	}
	concurrencyOverride := func(change func(*appconfig.Concurrency)) preferences.PartialDocument {
		candidate := inherited.Concurrency
		change(&candidate)
		converted := appConcurrencyToPreference(candidate)
		return preferences.PartialDocument{Concurrency: &converted}
	}
	deliveryOverride := func(change func(*appconfig.Delivery)) preferences.PartialDocument {
		candidate := inherited.Delivery
		change(&candidate)
		converted := appDeliveryToPreference(candidate)
		return preferences.PartialDocument{Delivery: &converted}
	}
	tests := []struct {
		name string
		want string
		doc  preferences.PartialDocument
	}{
		{"target score", "fix target score", fixOverride(func(value *appconfig.FixDefaults) { value.TargetScore++ })},
		{"focus removal", "remove inherited fix focus", fixOverride(func(value *appconfig.FixDefaults) { value.Focus = nil })},
		{"write scope", "fix change scope", fixOverride(func(value *appconfig.FixDefaults) { value.ChangeScope = "repository" })},
		{"profile", "user-owned fix profile", fixOverride(func(value *appconfig.FixDefaults) { value.Profile = "repository-profile" })},
		{"model", "user-owned fix model", fixOverride(func(value *appconfig.FixDefaults) { value.Model = "repository-model" })},
		{"effort", "user-owned fix effort", fixOverride(func(value *appconfig.FixDefaults) { value.Effort = "repository-effort" })},
		{"prompt", "user-owned fix prompt", fixOverride(func(value *appconfig.FixDefaults) { value.PromptTemplate = "repository-prompt" })},
		{"agent slots", "concurrency max agents", concurrencyOverride(func(value *appconfig.Concurrency) { value.MaxAgents++ })},
		{"verifier slots", "concurrency max verifiers", concurrencyOverride(func(value *appconfig.Concurrency) { value.MaxVerifiers++ })},
		{"actors per job", "concurrency actors per job", concurrencyOverride(func(value *appconfig.Concurrency) { value.MaxActorsPerJob++ })},
		{"delivery plan", "delivery plan", deliveryOverride(func(value *appconfig.Delivery) {
			value.DefaultPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitCommitCurrent, Publish: fix.PublishPullRequest}
		})},
		{"delivery remote", "user-owned delivery remote", deliveryOverride(func(value *appconfig.Delivery) { value.Remote = "upstream" })},
		{"delivery base", "user-owned delivery base", deliveryOverride(func(value *appconfig.Delivery) { value.BaseBranch = "develop" })},
		{"delivery branch template", "user-owned delivery branch template", deliveryOverride(func(value *appconfig.Delivery) { value.BranchTemplate = "repo/{job-short-id}" })},
		{"delivery publisher", "user-owned delivery publisher", deliveryOverride(func(value *appconfig.Delivery) { value.Publisher = "other" })},
		{"published pull request", "draft pull request policy", deliveryOverride(func(value *appconfig.Delivery) { value.DraftPullRequests = false })},
		{"commit title", "user-owned delivery commit title", deliveryOverride(func(value *appconfig.Delivery) { value.CommitTitleTemplate = "repo title" })},
		{"commit body", "user-owned delivery commit body", deliveryOverride(func(value *appconfig.Delivery) { value.CommitBodyTemplate = "repo body" })},
		{"pull request title", "user-owned delivery pull request title", deliveryOverride(func(value *appconfig.Delivery) { value.PullRequestTitleTemplate = "repo title" })},
		{"pull request body", "user-owned delivery pull request body", deliveryOverride(func(value *appconfig.Delivery) { value.PullRequestBodyTemplate = "repo body" })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRepositoryOverride(inherited, test.doc)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRepositoryOverride() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRepositoryBroadeningFromFileIsRejected(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	candidate := initial.Concurrency
	candidate.MaxAgents++
	preference := appConcurrencyToPreference(candidate)
	repositoryPath, _ := adapter.repositoryPreferencesPath(workspace)
	if err := preferences.SavePartial(repositoryPath, preferences.PartialDocument{Concurrency: &preference}); err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatalf("invalid repository preferences disabled resolution: %v", err)
	}
	if resolved.Concurrency != initial.Concurrency {
		t.Fatalf("invalid repository preferences changed effective configuration: %#v", resolved.Concurrency)
	}
}

func TestRepositoryBroadeningPatchIsNotPersisted(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	candidate := initial.Concurrency
	candidate.MaxAgents++
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Concurrency: &candidate}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "cannot broaden concurrency max agents") {
		t.Fatalf("Save() error = %v", err)
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatalf("rejected repository patch was persisted: %v", err)
	}
	if resolved.Concurrency != initial.Concurrency {
		t.Fatalf("resolved concurrency = %#v, want %#v", resolved.Concurrency, initial.Concurrency)
	}
}

func TestLargePositiveOperationalValuesAreAccepted(t *testing.T) {
	t.Parallel()
	const (
		largeSlots  = 10_000
		largeLines  = 1_000_000
		largeOutput = int64(1 << 48)
	)
	defaults := preferences.DefaultDocument()
	defaults.Concurrency = preferences.Concurrency{
		MaxAgents: largeSlots, MaxVerifiers: largeSlots,
		MaxActorsPerJob: largeSlots, MaxCandidatePreviewBytes: largeOutput, MaxCandidatePreviewLines: largeLines,
	}
	adapter, err := New(Options{UserPath: filepath.Join(t.TempDir(), "preferences.toml"), Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.Resolve(context.Background(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatalf("large positive values were rejected: %v", err)
	}
	if resolved.Concurrency.MaxAgents != largeSlots || resolved.Concurrency.MaxVerifiers != largeSlots ||
		resolved.Concurrency.MaxActorsPerJob != largeSlots || resolved.Concurrency.MaxCandidatePreviewBytes != largeOutput {
		t.Fatalf("large positive values were not preserved: %#v", resolved)
	}
}

func TestPositiveOperationalValuesRemainRequired(t *testing.T) {
	t.Parallel()
	defaults := preferences.DefaultDocument()
	defaults.Concurrency.MaxAgents = 0
	adapter, err := New(Options{UserPath: filepath.Join(t.TempDir(), "preferences.toml"), Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Resolve(context.Background(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{}); err == nil || !strings.Contains(err.Error(), "greater than zero") {
		t.Fatalf("non-positive operational value error = %v", err)
	}
}

func TestUserAuthenticationReferenceCannotContainLiteralSecret(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "sk-literal"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "literal credentials") {
		t.Fatalf("literal credential Save() error = %v", err)
	}
}

func TestInvalidProviderProfileCanBeLoadedForRepairAndSaved(t *testing.T) {
	root := t.TempDir()
	defaults := preferences.DefaultDocument()
	defaults.Agents.Profiles = []preferences.AgentProfile{{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{"unknown": "bad"}}}
	defaults.Fix.Profile = "codex"
	catalog := &strictProfileCatalog{}
	adapter, err := New(Options{UserPath: filepath.Join(root, "preferences.toml"), Defaults: defaults, RuntimeKinds: catalog.Kinds(), ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{RepositoryRoot: filepath.Join(root, "repo")}
	if _, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{}); err == nil {
		t.Fatal("invalid provider option resolved")
	}
	editable, err := adapter.LoadEditable(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(editable.Diagnostics) == 0 {
		t.Fatal("repair snapshot omitted diagnostics")
	}
	profiles := editable.Resolved.Profiles
	delete(profiles[0].Options, "unknown")
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, editable.Resolved.Revision); err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil || len(resolved.Profiles) != 1 {
		t.Fatalf("repaired resolve=%+v err=%v", resolved, err)
	}
}

func TestProviderValidationRunsForProfileWithoutOptions(t *testing.T) {
	root := t.TempDir()
	catalog := &rejectingProfileCatalog{}
	adapter, err := New(Options{UserPath: filepath.Join(root, "preferences.toml"), RuntimeKinds: catalog.Kinds(), ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{RepositoryRoot: filepath.Join(root, "repo")}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"}}
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, resolved.Revision); err == nil || !strings.Contains(err.Error(), "provider rejected") {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestOriginsIdentifyProfileOptionsAndListEntries(t *testing.T) {
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "primary", Label: "Codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{"sandbox": "strict"}}}
	fixDefaults := initial.Fix
	fixDefaults.Focus = []fix.MetricID{"cog"}
	saved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles, Fix: &fixDefaults}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"agents.primary", "agents.primary.executable", "agents.primary.options.sandbox", "fix.focus.cog"} {
		if got := saved.Resolved.Origins[key]; got != appconfig.OriginUser {
			t.Errorf("origin[%q] = %q, want user", key, got)
		}
	}
}

type rejectingProfileCatalog struct{}

func (*rejectingProfileCatalog) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*rejectingProfileCatalog) Descriptor(agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	return agent.ProfileDescriptor{Runtime: "codex-cli", Label: "Codex"}, nil
}
func (*rejectingProfileCatalog) ValidateProfile(agent.Profile) error {
	return errors.New("provider rejected profile")
}

type strictProfileCatalog struct{}

func (*strictProfileCatalog) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*strictProfileCatalog) Descriptor(agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	return agent.ProfileDescriptor{Runtime: "codex-cli", Label: "Codex"}, nil
}

type apiProfileCatalog struct{}

func (*apiProfileCatalog) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"openai-responses"} }
func (*apiProfileCatalog) Descriptor(kind agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	return agent.ProfileDescriptor{Runtime: kind, Label: "OpenAI Responses API", Fields: []agent.ProfileField{{
		Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Required: true,
	}}}, nil
}
func (*apiProfileCatalog) ValidateProfile(profile agent.Profile) error {
	if profile.Executable != "" {
		return errors.New("API profile cannot have an executable")
	}
	return nil
}
func (*strictProfileCatalog) ValidateProfile(profile agent.Profile) error {
	for key := range profile.Options {
		if key != "region" {
			return fmt.Errorf("unsupported option %s", key)
		}
	}
	return nil
}

func newTestAdapter(t *testing.T) (*Adapter, string, fix.WorkspaceIdentity) {
	t.Helper()
	root := t.TempDir()
	userPath := filepath.Join(root, "user", "preferences.toml")
	adapter, err := New(Options{UserPath: userPath, RuntimeKinds: []agent.RuntimeKind{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: filepath.Join(root, "repository")}
	return adapter, userPath, workspace
}
